/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"net/http"

	"github.com/emicklei/go-restful"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/klog/v2"
)

// InstallHandlers set router and handlers.
func (s *Server) InstallHandlers() {
	ws := new(restful.WebService)
	ws.Path("/pods").
		Produces(restful.MIME_JSON)
	ws.Route(ws.GET("").
		To(s.proxy).
		Operation("getPods"))
	s.restfulCont.Add(ws)

	ws = new(restful.WebService)
	ws.Path("/run")
	ws.Route(ws.POST("/{podNamespace}/{podID}/{containerName}").
		To(s.proxy).
		Operation("getRun"))
	ws.Route(ws.POST("/{podNamespace}/{podID}/{uid}/{containerName}").
		To(s.proxy).
		Operation("getRun"))
	s.restfulCont.Add(ws)

	ws = new(restful.WebService)
	ws.Path("/logs/")
	ws.Route(ws.GET("").
		To(s.proxy).
		Operation("getLogs"))
	ws.Route(ws.GET("/{logpath:*}").
		To(s.proxy).
		Operation("getLogs").
		Param(ws.PathParameter("logpath", "path to the log").DataType("string")))
	s.restfulCont.Add(ws)

	ws = new(restful.WebService)
	ws.Path("/containerLogs")
	ws.Route(ws.GET("/{podNamespace}/{podID}/{containerName}").
		To(s.proxy).
		Operation("getContainerLogs"))
	s.restfulCont.Add(ws)

	ws = new(restful.WebService)
	ws.Path("/exec")
	ws.Route(ws.GET("/{podNamespace}/{podID}/{containerName}").
		To(s.proxy).
		Operation("getExec"))
	ws.Route(ws.POST("/{podNamespace}/{podID}/{containerName}").
		To(s.proxy).
		Operation("getExec"))
	ws.Route(ws.GET("/{podNamespace}/{podID}/{uid}/{containerName}").
		To(s.proxy).
		Operation("getExec"))
	ws.Route(ws.POST("/{podNamespace}/{podID}/{uid}/{containerName}").
		To(s.proxy).
		Operation("getExec"))
	s.restfulCont.Add(ws)

	ws = new(restful.WebService)
	ws.Path("/attach")
	ws.Route(ws.GET("/{podNamespace}/{podID}/{containerName}").
		To(s.proxy).
		Operation("getAttach"))
	ws.Route(ws.POST("/{podNamespace}/{podID}/{containerName}").
		To(s.proxy).
		Operation("getAttach"))
	ws.Route(ws.GET("/{podNamespace}/{podID}/{uid}/{containerName}").
		To(s.proxy).
		Operation("getAttach"))
	ws.Route(ws.POST("/{podNamespace}/{podID}/{uid}/{containerName}").
		To(s.proxy).
		Operation("getAttach"))
	s.restfulCont.Add(ws)

	ws = new(restful.WebService)
	ws.Path("/portForward")
	ws.Route(ws.GET("/{podNamespace}/{podID}").
		To(s.proxy).
		Operation("getPortForward"))
	ws.Route(ws.POST("/{podNamespace}/{podID}").
		To(s.proxy).
		Operation("getPortForward"))
	ws.Route(ws.GET("/{podNamespace}/{podID}/{uid}").
		To(s.proxy).
		Operation("getPortForward"))
	ws.Route(ws.POST("/{podNamespace}/{podID}/{uid}").
		To(s.proxy).
		Operation("getPortForward"))
	s.restfulCont.Add(ws)
}

func (s *Server) proxy(req *restful.Request, resp *restful.Response) {
	klog.V(4).Infof("request %+v", req.Request.URL)

	// there must be a peer certificate in the tls connection
	if req.Request.TLS == nil || len(req.Request.TLS.PeerCertificates) == 0 {
		resp.ResponseWriter.WriteHeader(http.StatusForbidden)
		return
	}

	if s.config.KubeletClientCert != nil {
		klog.Info("will forward request to kubelet")
		// forward request to kubelet
		req.Request.URL.Host = s.config.KubeletServerHost
		req.Request.URL.Scheme = "https"

		tenantName := req.Request.TLS.PeerCertificates[0].Subject.CommonName
		TranslatePath(req, tenantName)

		klog.V(4).Infof("request after translate %+v", req.Request.URL)
	} else {
		klog.Info("will forward request to super apiserver")
		// forward request to super apiserver
		tenantName := req.Request.TLS.PeerCertificates[0].Subject.CommonName
		err := TranslatePathForSuper(req, tenantName)
		if err != nil {
			klog.Errorf("fail to translate url path for super master: %s", err)
			resp.ResponseWriter.WriteHeader(http.StatusNotFound)
			resp.ResponseWriter.Write([]byte(err.Error()))
			return
		}
		klog.V(4).Infof("request after translate %+v", req.Request.URL)

		// mutate the request, i.e., replacing the dst, add bearer token header
		req.Request.URL.Host = s.superAPIServerAddress.Host
		req.Request.URL.Scheme = "https"
		req.Request.Header.Add("Authorization", "Bearer "+s.restConfig.BearerToken)
	}

	handler := proxy.NewUpgradeAwareHandler(req.Request.URL, s.transport /*transport*/, false /*wrapTransport*/, httpstream.IsUpgradeRequest(req.Request) /*upgradeRequired*/, &responder{})
	handler.ServeHTTP(resp.ResponseWriter, req.Request)
}

type responder struct{}

func (r *responder) Error(w http.ResponseWriter, req *http.Request, err error) {
	klog.Errorf("Error while proxying request: %v", err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
