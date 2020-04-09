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
	"sync"
	"time"
)

type Manager struct {
	loginPlugins       []Plugin
	newProxyPlugins    []Plugin
	newUserConnPlugins []Plugin
	newUserConnChan    chan *NewUserConn
	newUserConnCache   map[string]map[string]bool
	initNewUserConn    sync.Once
}

func NewManager() *Manager {
	return &Manager{
		loginPlugins:       make([]Plugin, 0),
		newProxyPlugins:    make([]Plugin, 0),
		newUserConnPlugins: make([]Plugin, 0),
	}
}

func (m *Manager) Register(p Plugin) {
	if p.IsSupport(OpLogin) {
		m.loginPlugins = append(m.loginPlugins, p)
	}
	if p.IsSupport(OpNewProxy) {
		m.newProxyPlugins = append(m.newProxyPlugins, p)
	}
	if p.IsSupport(OpNewUserConn) {
		m.newUserConnPlugins = append(m.newUserConnPlugins, p)
		m.initNewUserConn.Do(func() {
			m.newUserConnChan = make(chan *NewUserConn, 1000)
			m.newUserConnCache = make(map[string]map[string]bool)
			m.doNotifyNewUserConn()
		})
	}
}

func (m *Manager) Login(content *LoginContent) (*LoginContent, error) {
	if len(m.loginPlugins) == 0 {
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
	if len(m.newProxyPlugins) == 0 {
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

func (m *Manager) NewUserConn(userConn *NewUserConn) {
	if len(m.newUserConnPlugins) == 0 {
		return
	}

	select {
	case m.newUserConnChan <- userConn:
	default:
		xl := xlog.New()
		xl.Warn("reach max send buffer")
	}
}

func (m *Manager) doNotifyNewUserConn() {
	xl := xlog.New().AppendPrefix("NewUserConn")
	go func() {
		clearTicker := time.NewTicker(time.Hour * 24)
		for {
			select {
			case userConn := <-m.newUserConnChan:
				xl.Debug("received NewUserConn [%s:%s %s]", userConn.ProxyName, userConn.ProxyType, userConn.RemoteIp)
				userConnMap, ok := m.newUserConnCache[userConn.ProxyName]
				if !ok {
					userConnMap = make(map[string]bool)
				}
				if _, ok := userConnMap[userConn.RemoteIp]; !ok {
					// Notifies the IP of the first access proxy
					userConnMap[userConn.RemoteIp] = false
					for _, p := range m.newUserConnPlugins {
						_, _, err := p.Handle(context.Background(), OpNewUserConn, userConn)
						if err != nil {
							xl.Warn("send NewUserConn [%s:%s %s] request to plugin [%s] error: %v",
								userConn.ProxyName, userConn.ProxyType, userConn.RemoteIp, p.Name(), err)
						}
					}
				}
			case <-clearTicker.C:
				// clear the IP record cache
				xl.Debug("clear NewUserConn cache")
				m.newUserConnCache = make(map[string]map[string]bool)
			}
		}
	}()
}
