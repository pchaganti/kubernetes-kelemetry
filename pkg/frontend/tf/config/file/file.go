// Copyright 2023 The Kelemetry Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tfconfigfile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/yaml"

	tfconfig "github.com/kubewharf/kelemetry/pkg/frontend/tf/config"
	tfscheme "github.com/kubewharf/kelemetry/pkg/frontend/tf/scheme"
	"github.com/kubewharf/kelemetry/pkg/manager"
)

func init() {
	manager.Global.ProvideMuxImpl("jaeger-transform-config/file", manager.Ptr(&FileProvider{
		configs:        make(map[tfconfig.Id]*tfconfig.Config),
		nameToConfigId: make(map[string]tfconfig.Id),
	}), tfconfig.Provider.DefaultId)
}

type options struct {
	file string
}

func (options *options) Setup(fs *pflag.FlagSet) {
	fs.StringVar(&options.file, "jaeger-transform-config-file", "hack/tfconfig.yaml", "path to tfconfig file")
}

func (options *options) EnableFlag() *bool { return nil }

type FileProvider struct {
	manager.MuxImplBase

	Scheme tfscheme.Scheme

	options        options
	configs        map[tfconfig.Id]*tfconfig.Config
	nameToConfigId map[string]tfconfig.Id
	defaultConfig  tfconfig.Id
}

var (
	_ manager.Component = &FileProvider{}
	_ tfconfig.Provider = &FileProvider{}
)

func (p *FileProvider) MuxImplName() (name string, isDefault bool) { return "default", true }

func (p *FileProvider) Options() manager.Options { return &p.options }

func (p *FileProvider) Init() error {
	file, err := os.Open(p.options.file)
	if err != nil {
		return fmt.Errorf("cannot open tfconfig file: %w", err)
	}
	defer file.Close()

	yamlBytes, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read tfconfig error: %w", err)
	}

	jsonBytes, err := yaml.ToJSON(yamlBytes)
	if err != nil {
		return fmt.Errorf("parse tfconfig YAML error: %w", err)
	}

	if err := p.loadJsonBytes(jsonBytes); err != nil {
		return err
	}

	return nil
}

func (p *FileProvider) loadJsonBytes(jsonBytes []byte) error {
	type Batch struct {
		Name  string          `json:"name"`
		Steps json.RawMessage `json:"steps"`
	}

	var file struct {
		Batches []Batch `json:"batches"`
		Configs []struct {
			Id         tfconfig.Id     `json:"id"`
			Name       string          `json:"name"`
			UseSubtree bool            `json:"useSubtree"`
			Steps      json.RawMessage `json:"steps"`
		}
	}
	if err := json.Unmarshal(jsonBytes, &file); err != nil {
		return fmt.Errorf("parse tfconfig error: %w", err)
	}

	batches := map[string][]tfscheme.Step{}
	for _, batch := range file.Batches {
		steps, err := tfscheme.ParseSteps(batch.Steps, p.Scheme, batches)
		if err != nil {
			return fmt.Errorf("parse tfconfig batch error: %w", err)
		}

		batches[batch.Name] = steps
	}

	for _, raw := range file.Configs {
		steps, err := tfscheme.ParseSteps(raw.Steps, p.Scheme, batches)
		if err != nil {
			return fmt.Errorf("parse tfconfig step error: %w", err)
		}

		config := &tfconfig.Config{
			Id:         raw.Id,
			Name:       raw.Name,
			UseSubtree: raw.UseSubtree,
			Steps:      steps,
		}

		p.Register(config)
	}

	return nil
}

func (p *FileProvider) Register(config *tfconfig.Config) {
	p.configs[config.Id] = config
	p.nameToConfigId[config.Name] = config.Id
}

func (p *FileProvider) Start(ctx context.Context) error { return nil }
func (p *FileProvider) Close(ctx context.Context) error { return nil }

func (p *FileProvider) Names() []string {
	names := make([]string, 0, len(p.nameToConfigId))
	for name := range p.nameToConfigId {
		names = append(names, name)
	}
	return names
}

func (p *FileProvider) DefaultName() string { return p.configs[p.defaultConfig].Name }

func (p *FileProvider) DefaultId() tfconfig.Id { return p.defaultConfig }

func (p *FileProvider) GetByName(name string) *tfconfig.Config {
	id, exists := p.nameToConfigId[name]
	if !exists {
		return nil
	}
	return p.configs[id]
}

func (p *FileProvider) GetById(id tfconfig.Id) *tfconfig.Config { return p.configs[id] }
