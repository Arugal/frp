// Copyright 2019 fatedier, fatedier@gmail.com
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

package plugin

import (
	"context"
	"errors"
	"fmt"
	"github.com/fatedier/frp/utils/util"
	"github.com/fatedier/frp/utils/xlog"
	"strings"
	"time"
)

type Manager struct {
	enableLogin         bool
	loginPlugins        []Plugin
	enableNewProxy      bool
	newProxyPlugins     []Plugin
	enableTraceAccessIp bool
	newAccessIpPlugins  []Plugin
	accessIpChan        chan NewAccessIpContent
	accessIp            map[string]map[string]int
}

func NewManager() *Manager {
	manager := &Manager{
		loginPlugins:        make([]Plugin, 0),
		newProxyPlugins:     make([]Plugin, 0),
		newAccessIpPlugins:  make([]Plugin, 0),
		enableTraceAccessIp: false,
		accessIpChan:        make(chan NewAccessIpContent, 1000),
		accessIp:            make(map[string]map[string]int),
	}
	manager.doNotifyAccessIp()
	return manager
}

func (m *Manager) Register(p Plugin) {
	if p.IsSupport(OpLogin) {
		m.loginPlugins = append(m.loginPlugins, p)
		m.enableLogin = true
	}
	if p.IsSupport(OpNewProxy) {
		m.newProxyPlugins = append(m.newProxyPlugins, p)
		m.enableNewProxy = true
	}
	if p.IsSupport(OpNewAccessIp) {
		m.newAccessIpPlugins = append(m.newAccessIpPlugins, p)
		m.enableTraceAccessIp = true
	}
}

func (m *Manager) Login(content *LoginContent) (*LoginContent, error) {
	if !m.enableLogin {
		return content, nil
	}
	var (
		res = &Response{
			Reject:   false,
			Unchange: true,
		}
		retContent interface{}
		err        error
	)
	reqid, _ := util.RandId()
	xl := xlog.New().AppendPrefix("reqid: " + reqid)
	ctx := xlog.NewContext(context.Background(), xl)
	ctx = NewReqidContext(ctx, reqid)

	for _, p := range m.loginPlugins {
		res, retContent, err = p.Handle(ctx, OpLogin, *content)
		if err != nil {
			xl.Warn("send Login request to plugin [%s] error: %v", p.Name(), err)
			return nil, errors.New("send Login request to plugin error")
		}
		if res.Reject {
			return nil, fmt.Errorf("%s", res.RejectReason)
		}
		if !res.Unchange {
			content = retContent.(*LoginContent)
		}
	}
	return content, nil
}

func (m *Manager) NewProxy(content *NewProxyContent) (*NewProxyContent, error) {
	if !m.enableNewProxy {
		return content, nil
	}
	var (
		res = &Response{
			Reject:   false,
			Unchange: true,
		}
		retContent interface{}
		err        error
	)
	reqid, _ := util.RandId()
	xl := xlog.New().AppendPrefix("reqid: " + reqid)
	ctx := xlog.NewContext(context.Background(), xl)
	ctx = NewReqidContext(ctx, reqid)

	for _, p := range m.newProxyPlugins {
		res, retContent, err = p.Handle(ctx, OpNewProxy, *content)
		if err != nil {
			xl.Warn("send NewProxy request to plugin [%s] error: %v", p.Name(), err)
			return nil, errors.New("send NewProxy request to plugin error")
		}
		if res.Reject {
			return nil, fmt.Errorf("%s", res.RejectReason)
		}
		if !res.Unchange {
			content = retContent.(*NewProxyContent)
		}
	}
	return content, nil
}

func (m *Manager) TraceAccessIp(pxyName string, userRemoteAddr string) {
	if !m.enableTraceAccessIp {
		return
	}

	m.accessIpChan <- NewAccessIpContent{
		ProxyName:    pxyName,
		UserRemoteIp: strings.Split(userRemoteAddr, ":")[0],
	}
}

func (m *Manager) doNotifyAccessIp() {
	go func() {
		ticker := time.NewTicker(time.Hour * 24)
		for {
			select {
			case accessIp := <-m.accessIpChan:
				accessIpMap, ok := m.accessIp[accessIp.ProxyName]
				if !ok {
					accessIpMap = make(map[string]int)
					m.accessIp[accessIp.ProxyName] = accessIpMap
				}
				if _, ok := accessIpMap[accessIp.UserRemoteIp]; !ok {
					accessIpMap[accessIp.UserRemoteIp] = 1

					for _, p := range m.newAccessIpPlugins {
						_, _, _ = p.Handle(context.Background(), OpNewAccessIp, accessIp)
					}
				} else {
					accessIpMap[accessIp.UserRemoteIp] = accessIpMap[accessIp.UserRemoteIp] + 1
				}
			case <-ticker.C:
				m.accessIp = make(map[string]map[string]int)
			}
		}
	}()
}
