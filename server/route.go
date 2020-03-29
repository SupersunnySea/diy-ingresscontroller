package server

import (
	"github.com/sirupsen/logrus"
	"k8s.io/api/networking/v1beta1"
	"github.com/SupersunnySea/diy-ingresscontroller/watcher"
	"crypto/tls"
	"fmt"
	"net/url"
	"regexp"
)

type RoutingTable struct {
	certificateByHost map[string]map[string]*tls.Certificate
	backendByHost     map[string][]routingTableBackend
}

type routingTableBackend struct {
	pathRE *regexp.Regexp
	url    *url.URL
}

func newRoutingTableBackend(path, host string, port int) (*routingTableBackend, error) {
	rtb := &routingTableBackend{
		url: &url.URL{
			Scheme: "http",
			Host:   fmt.Sprintf("%s:%d", host, port),
		},
	}
	var err error
	if path != "" {
		rtb.pathRE, err = regexp.Compile(path)
	}
	return rtb, err
}

func (rtb routingTableBackend) matches(path string) bool {
	if rtb.pathRE == nil {
		return true //???为什么是true
	}
	return rtb.pathRE.MatchString(path)
}

//根据payload生成路由表
func NewRoutingTable(payload *watcher.Payload)*RoutingTable{
	rt:=&RoutingTable{
		certificateByHost:make(map[string]map[string]*tls.Certificate),
		backendByHost:make(map[string][]routingTableBackend)
	}

}

func (rt *RoutingTable)init(payload *Watcher.Payload){
	if payload==nil{
		return
	}
	//payload{ map=TLS,[]ingresspayload}
	//ingresspayload{ingress,serviceport}
	for _,ingressPayload:=range payload.Ingresses{
		for _,rule:=range ingressPayload.Ingress.Spec.Rules{
			m,ok:=rt.certificateByHost[rule.Host]
			if !ok{
				m=make(map(string)*tls.Certificate)
				rt.certificateByHost[rule.Host]=m
			}
			for _,t:=range ingressPayload.Ingress.Spec.TLS{
				for _,h:=range t.Hosts{
					cert,ok:=payload.TLSCertificates[t.SecretName]
					if ok{
						m[h]=cert
					}
				}
			}
			rt.addBackend(ingressPayload,rule)
		}
	}
}

func (rt *RoutingTable)addBackend(ingressPayload *Watcher.IngressPayload,rule v1beta1.IngressRule){
	if rule.HTTP==nil{
		if ingressPayload.Ingress.Spec.Backend!=nil{
			backend:=ingressPayload.Ingress.Spec.Backend
			rtb,err:=newRoutingTableBackend("",backend.ServiceName,backend.ServicePort)
			if err!=nil{
				logrus.Errorf("new routingtablebackend err:%s",err.Error())
				return
			}
			rt.backendByHost[rule.Host]=append(rt.backendByHost[rule.Host],rtb)
		}
	}else{
		for _,path:=rule.HTTP.Paths{
			backend:=path.Backend
			rtb,err:=newRoutingTableBackend(path.Path,backend.ServiceName,backend.ServicePort)
			if err!=nil{
				logrus.Errorf("new routingtablebackend err:%s",err.Error())
				return
			}
			rt.backendByHost[rule.Host]=append(rt.backendByHost[rule.Host],rtb)
		}
	}
}

func (rt *RoutingTable)GetCertificate(host string)(*tls.Certificate,error){
	hostcerts,ok:=rt.certificateByHost[host]
	if !ok{
		return nil,err
	}
	for h,cert:=range hostcerts{
		if h==host{
			return cert,nil
		}
	}
}

func (rt *RoutingTable)GetBackend(host,path string)(*url.URL,error){
	backends,ok:=rt.backendByHost[host]
	if !ok{
		return nil,err
	}
	for _,backend:=range backends{
		if backend.pathRE==nil{
			return backend.url,nil
		}
		if rtb.pathRE.MatchString(path){
			return backend.url,nil
		}
	}
	return nil,errors.New("backend not found!!!")
}