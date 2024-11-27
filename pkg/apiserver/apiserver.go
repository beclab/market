// Copyright 2022 bytetrade
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

package apiserver

import (
	"market/internal/conf"
	"market/internal/constants"
	"market/internal/monitor"
	"market/internal/redisdb"
	servicev1 "market/pkg/apiserver/service/v1"
	"net/http"

	restfulspec "github.com/emicklei/go-restful-openapi/v2"
	"github.com/emicklei/go-restful/v3"
	"github.com/go-openapi/spec"
	"github.com/golang/glog"
)

type APIServer struct {
	Server *http.Server

	// RESTful Server
	container *restful.Container
}

func New() (*APIServer, error) {
	as := &APIServer{}

	server := &http.Server{
		Addr: constants.APIServerListenAddress,
	}

	as.Server = server
	return as, nil
}

func (s *APIServer) PrepareRun() error {
	conf.Init()

	s.container = restful.NewContainer()
	s.container.Filter(logRequestAndResponse)
	s.container.Router(restful.CurlyRouter{})
	s.container.RecoverHandler(func(panicReason interface{}, httpWriter http.ResponseWriter) {
		logStackOnRecover(panicReason, httpWriter)
	})

	s.installModuleAPI()
	s.installAPIDocs()

	for _, ws := range s.container.RegisteredWebServices() {
		glog.Infof("registered module: %s", ws.RootPath())
	}

	s.Server.Handler = s.container

	if conf.GetIsPublic() {
		return nil
	}
	err := redisdb.Init()
	if err != nil {
		glog.Fatalf("redisdb init err%s", err.Error())
	}

	glog.Info("call monitor Start")
	go monitor.Start()

	return nil
}

func (s *APIServer) Run() error {
	return s.Server.ListenAndServe()
}

func (s *APIServer) installAPIDocs() {
	config := restfulspec.Config{
		WebServices:                   s.container.RegisteredWebServices(), // you control what services are visible
		APIPath:                       "/market/v1/apidocs.json",
		PostBuildSwaggerObjectHandler: enrichSwaggerObject}
	s.container.Add(restfulspec.NewOpenAPIService(config))

	cors := restful.CrossOriginResourceSharing{
		AllowedHeaders: []string{"Content-Type", "Accept"},
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
		CookiesAllowed: false,
		Container:      restful.DefaultContainer}
	s.container.Filter(cors.Filter)
}

func (s *APIServer) installModuleAPI() {
	_ = servicev1.AddToContainer(s.container)
}

func enrichSwaggerObject(swo *spec.Swagger) {
	swo.Info = &spec.Info{
		InfoProps: spec.InfoProps{
			Title:       "App Store",
			Description: "Backend For App Store",
			Contact: &spec.ContactInfo{
				ContactInfoProps: spec.ContactInfoProps{
					Name:  "bytetrade",
					Email: "dev@bytetrade.io",
					URL:   "http://bytetrade.io",
				},
			},
			License: &spec.License{
				LicenseProps: spec.LicenseProps{
					Name: "Apache License 2.0",
					URL:  "http://www.apache.org/licenses/LICENSE-2.0",
				},
			},
			Version: "1.0.0",
		},
	}
	swo.Tags = []spec.Tag{{TagProps: spec.TagProps{
		Name:        "App Store",
		Description: "App Store Demo"}}}
	swo.Schemes = []string{"http", "https"}
}
