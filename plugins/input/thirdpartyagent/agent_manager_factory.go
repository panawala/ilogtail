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
	"fmt"
	"path"

	"sync"

	pluginmanager "github.com/alibaba/ilogtail/pluginmanager"
)

var AgentManagerFactory *agentManagerFactory = new(agentManagerFactory)

type agentManagerFactory struct {
	cachedAgentManager map[string]*AgentManager
	lock               sync.RWMutex
}

func (amf *agentManagerFactory) GetCachedManager(agentName string) *AgentManager {
	amf.lock.RLock()
	defer amf.lock.RUnlock()
	if amf.cachedAgentManager == nil {
		return nil
	}
	if agentManager, exist := amf.cachedAgentManager[agentName]; exist {
		return agentManager
	}
	return nil
}

func (amf *agentManagerFactory) SetCachedManager(agentName string, agentManager *AgentManager) {
	amf.lock.Lock()
	defer amf.lock.Unlock()
	if amf.cachedAgentManager == nil {
		amf.cachedAgentManager = make(map[string]*AgentManager, 1)
	}
	amf.cachedAgentManager[agentName] = agentManager
}

func (amf *agentManagerFactory) GetAgentManager(agentName string) *AgentManager {
	agentManager := amf.GetCachedManager(agentName)
	if agentManager != nil {
		return agentManager
	}

	agentDirPath := path.Join(pluginmanager.LogtailGlobalConfig.LogtailSysConfDir, "thirdpartyagent")
	agentManager = &AgentManager{
		agentName:        agentName,
		configs:          make(map[string]*Config),
		loadedConfigs:    make(map[string]*Config),
		configChanged:    make(chan struct{}, 1),
		agentBasePath:    agentDirPath,
		agentdPath:       path.Join(agentDirPath, agentName, fmt.Sprintf("%sd", agentName)),
		agentConfDirPath: path.Join(agentDirPath, agentName, "logtail_conf"),
	}
	go agentManager.Run()
	amf.SetCachedManager(agentName, agentManager)
	return agentManager
}
