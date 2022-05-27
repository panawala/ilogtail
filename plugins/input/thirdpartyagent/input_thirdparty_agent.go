// Copyright 2021 iLogtail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package thirdpartyagent

import (
	"encoding/json"
	"fmt"

	"crypto/md5" //nolint:gosec
	"github.com/alibaba/ilogtail"
	"github.com/alibaba/ilogtail/pkg/logger"
)

type (
	AgentConfig struct {
		SingleFile  bool
		FileName    string
		FileContent string
	}

	ServiceThirdpartyAgent struct {
		Agent       string
		AgentConfig *AgentConfig

		config  *Config
		am      *AgentManager
		context ilogtail.Context
	}
)

func (a *AgentConfig) Hash() string {
	aBytes, _ := json.Marshal(a)
	return fmt.Sprintf("%x", md5.Sum(aBytes))
}

func (s *ServiceThirdpartyAgent) Init(ctx ilogtail.Context) (int, error) {
	if s.AgentConfig == nil {
		return 0, fmt.Errorf("agent config is empty")
	}
	s.context = ctx

	configName := fmt.Sprintf("%s_%s", ctx.GetConfigName(), s.AgentConfig.FileName)
	if s.AgentConfig.SingleFile {
		configName = s.AgentConfig.FileName
	}
	s.config = &Config{
		Name:        configName,
		AgentConfig: s.AgentConfig,
	}
	logger.Info(s.context.GetRuntimeContext(), "configName is", ctx.GetConfigName())

	s.am = AgentManagerFactory.GetAgentManager(s.Agent)

	return 0, nil
}

func (s *ServiceThirdpartyAgent) Description() string {
	return "service for Thirdparty agent"
}

func (s *ServiceThirdpartyAgent) Collect(collector ilogtail.Collector) error {
	logger.Info(s.context.GetRuntimeContext(), "collecter ServiceThirdpartyAgent")
	return nil
}

func (s *ServiceThirdpartyAgent) Start(collector ilogtail.Collector) error {
	logger.Info(s.context.GetRuntimeContext(), "start the ServiceThirdpartyAgent plugin")
	s.am.RegisterConfig(s.config)
	return nil
}

func (s *ServiceThirdpartyAgent) Stop() error {
	logger.Info(s.context.GetRuntimeContext(), "stop the ServiceThirdpartyAgent plugin")
	s.am.UnregisterConfig(s.config)
	return nil
}

func init() {
	ilogtail.ServiceInputs["service_thirdparty_agent"] = func() ilogtail.ServiceInput {
		return &ServiceThirdpartyAgent{}
	}
}
