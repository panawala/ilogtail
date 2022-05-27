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
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/alibaba/ilogtail/pkg/logger"
)

var statusCheckInterval = time.Second * time.Duration(30)

type Config struct {
	Name        string
	AgentConfig *AgentConfig
}

// Telegraf supervisor for agent start, stop, config reload...
//
// Because Telegraf will send all inputs' data to all outputs, so only ONE Logtail
//   config will be passed to Telegraf simultaneously.
//
// Data link: Agent ------ HTTP/File ------> Logtail ----- Protobuf ------> SLS.
// Logtail will work as an InfluxDB server to receive data from telegraf by HTTP protocol.
type AgentManager struct {
	// Although only one config will be loaded, all configs should be saved for replacement
	// while current config is unregistered.
	configs map[string]*Config
	mu      sync.Mutex

	loadedConfigs map[string]*Config

	configChanged    chan struct{}
	agentName        string
	agentBasePath    string
	agentdPath       string
	agentConfDirPath string
}

func (am *AgentManager) RegisterConfig(c *Config) {
	am.mu.Lock()
	am.configs[c.Name] = c
	am.mu.Unlock()
	logger.Debugf(context.Background(), "register config: %v", c)
	am.notify()
}

func (am *AgentManager) UnregisterConfig(c *Config) {
	am.mu.Lock()
	delete(am.configs, c.Name)
	am.mu.Unlock()
	logger.Debugf(context.Background(), "unregister config: %v", c)
	am.notify()
}

func (am *AgentManager) notify() {
	select {
	case am.configChanged <- struct{}{}:
	default:
	}
}

func (am *AgentManager) Run() {
	am.initAgentConfigDir()
	logger.Infof(context.Background(), "install agent dir")

	for {
		select {
		case <-time.After(statusCheckInterval):
		case <-am.configChanged:
		}

		logger.Debugf(context.Background(), "start to check")
		am.reconcile()
		logger.Debugf(context.Background(), "check done")
	}
}

func (am *AgentManager) reconcile() {
	configIsEmpty, noConfigChanged := am.reconcileConfigFile()
	if configIsEmpty {
		am.stop()
		return
	}

	if !am.makeSureInstalled() {
		return
	}

	if noConfigChanged {
		am.start()
	} else {
		am.reload()
	}
}

func (am *AgentManager) start() {
	logger.Infof(context.Background(), "agent config start")
	_, _ = am.runAgentd(true, "start")
}

func (am *AgentManager) stop() {
	logger.Infof(context.Background(), "agent config stop")
	if exist, _ := isPathExist(am.agentdPath); !exist {
		return
	}
	_, _ = am.runAgentd(true, "stop")
}

func (am *AgentManager) reload() {
	logger.Infof(context.Background(), "agent config reloaded")
	_, _ = am.runAgentd(true, "reload")
}

// reconcileConfigFile returns true no change in config.
func (am *AgentManager) reconcileConfigFile() (configIsEmpty, noConfigChanged bool) {
	configs := am.getLatestConfigs()
	logger.Debugf(context.Background(), "latest configs: %v", configs)

	// Clear all loaded config files and stop agent.
	if configs == nil {
		if len(am.loadedConfigs) > 0 {
			for name := range am.loadedConfigs {
				am.removeConfigFile(name)
			}
			logger.Infof(context.Background(), "clear all configs and stop agent %s, count: %v", am.agentName, len(am.loadedConfigs))
			am.loadedConfigs = make(map[string]*Config)
		}
		configIsEmpty = true
		return
	}

	// Still have configs, do comparison.
	toRemoveConfigs := make([]string, 0)
	toAddOrUpdateConfigs := make([]*Config, 0)

	for name, curCfg := range am.loadedConfigs {
		if cfg, exists := configs[name]; exists {
			if curCfg.AgentConfig.Hash() != cfg.AgentConfig.Hash() {
				toAddOrUpdateConfigs = append(toAddOrUpdateConfigs, cfg)
			}
		} else {
			toRemoveConfigs = append(toRemoveConfigs, name)
		}
	}
	for name, cfg := range configs {
		if _, exists := am.loadedConfigs[name]; !exists {
			toAddOrUpdateConfigs = append(toAddOrUpdateConfigs, cfg)
		}
	}

	for _, name := range toRemoveConfigs {
		am.removeConfigFile(name)
		delete(am.loadedConfigs, name)
	}
	for _, cfg := range toAddOrUpdateConfigs {
		if am.overwriteConfigFile(cfg) {
			am.loadedConfigs[cfg.Name] = cfg
		}
	}
	noConfigChanged = len(toRemoveConfigs) == 0 && len(toAddOrUpdateConfigs) == 0
	return
}

func (am *AgentManager) getLatestConfigs() map[string]*Config {
	am.mu.Lock()
	defer am.mu.Unlock()

	if len(am.configs) == 0 {
		return nil
	}

	toLoadConfigs := make(map[string]*Config)
	for name, cfg := range am.configs {
		toLoadConfigs[name] = cfg
	}
	return toLoadConfigs
}

func (am *AgentManager) overwriteConfigFile(cfg *Config) bool {
	filePath := path.Join(am.agentConfDirPath, "conf.d")
	if _, err := makeSureDirectoryExist(filePath); err != nil {
		logger.Warningf(context.Background(), "SERVICE_AGENT_OVERWRITE_CONFIG_ALARM",
			"overwrite local config file error, path: %v err: %v", filePath, err)
		return false
	}
	fileName := path.Join(filePath, cfg.Name)
	if err := ioutil.WriteFile(fileName, []byte(cfg.AgentConfig.FileContent), 0600); err != nil {
		logger.Warningf(context.Background(), "SERVICE_AGENT_OVERWRITE_CONFIG_ALARM",
			"overwrite local config file error, path: %v err: %v", filePath, err)
		return false
	}

	logger.Infof(context.Background(), "overwrite agent config %v", cfg.Name)
	return true
}

func (am *AgentManager) removeConfigFile(name string) {
	filePath := path.Join(am.agentConfDirPath, "conf.d", name)
	if err := os.Remove(filePath); err != nil {
		logger.Warningf(context.Background(), "SERVICE_AGENT_REMOVE_CONFIG_ALARM",
			"remove local config file error, path: %v, err: %v", filePath, err)
		return
	}

	logger.Infof(context.Background(), "remove agent config %v", name)
}

func (am *AgentManager) runAgentd(needOutput bool, commandArgs ...string) (output []byte, err error) {
	cmd := exec.Command(am.agentdPath, commandArgs...) //nolint:gosec
	if needOutput {
		output, err = cmd.CombinedOutput()
	} else {
		// Must call start/reload without output, because they might fork sub process,
		// which will hang when CombinedOutput is called.
		err = cmd.Run()
	}
	// Workaround: exec.Command throws wait:no child process error always under c-shared buildmode.
	// TODO: try cgo, implement exec with C and popen.
	if err != nil && !strings.Contains(err.Error(), "no child process") {
		logger.Warningf(context.Background(), "SERVICE_AGENT_RUNTIME_ALARM",
			"%v error, output: %v, error: %v", strings.Join(commandArgs, " "), string(output), err)
	}
	return
}

// makeSureInstalled returns true if agent has been installed.
func (am *AgentManager) makeSureInstalled() bool {
	logger.Debug(context.Background(), "Installing", am.agentdPath)
	if exist, err := isPathExist(am.agentdPath); err != nil {
		logger.Warningf(context.Background(), "SERVICE_AGENT_RUNTIME_ALARM",
			"stat path %v err when install: %v", am.agentdPath, err)
		return false
	} else if exist {
		logger.Debug(context.Background(), "Ignore Installing", am.agentdPath)
		return true
	}

	installScriptPath := path.Join(am.agentBasePath, "install_agent.sh")
	if exist, err := isPathExist(installScriptPath); err != nil || !exist {
		logger.Warningf(context.Background(), "SERVICE_AGENT_RUNTIME_ALARM",
			"can not find install script %v, maybe stat error: %v", installScriptPath, err)
		return false
	}

	// Install by execute install.sh
	cmd := exec.Command(installScriptPath, am.agentName) //nolint:gosec
	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(err.Error(), "no child process") {
		logger.Warningf(context.Background(), "SERVICE_AGENT_RUNTIME_ALARM",
			"install agent error, output: %v, error: %v", string(output), err)
		return false
	}
	am.initAgentConfigDir()
	logger.Infof(context.Background(), "install agent done, reinit loadedConfigs, output: %v", string(output))
	am.loadedConfigs = make(map[string]*Config)
	am.reconcileConfigFile()
	return true
}

func (am *AgentManager) initAgentConfigDir() {
	if newDir, err := makeSureDirectoryExist(am.agentConfDirPath); newDir {
		if err != nil {
			logger.Warningf(context.Background(), "SERVICE_AGENT_RUNTIME_ALARM",
				"create conf dir error, path %v, err: %v", am.agentConfDirPath, err)
		}
		return
	}
	// Clean config files (outdated) in conf directory.
	files, err := ioutil.ReadDir(am.agentConfDirPath)
	if err != nil {
		logger.Warningf(context.Background(), "SERVICE_AGENT_RUNTIME_ALARM",
			"clean conf dir error, path %v, err: %v", am.agentConfDirPath, err)
		return
	}
	for _, f := range files {
		filePath := path.Join(am.agentConfDirPath, f.Name())
		if err = os.Remove(filePath); err == nil {
			logger.Infof(context.Background(), "delete outdated agent config file: %v", filePath)
		} else {
			logger.Warningf(context.Background(), "deleted outdated agent config file err, path: %v, err: %v",
				filePath, err)
		}
	}
}
