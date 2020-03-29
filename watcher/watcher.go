package watcher

import (
	"context"
	"crypto/tls"
	"sync"
	"time"

	"k8s.io/client-go/tools/cache"

	"github.com/bep/debounce"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

type Watcher struct {
	client   *kubernetes.Clientset
	onChange func(*Payload)
}

type Payload struct {
	Ingresses       []*IngressPayload
	TLSCertificates map[string]*tls.Certificate //key:secret name,value:certificate
}

type IngressPayload struct {
	Ingress      *v1beta1.Ingress
	ServicePorts map[string]map[string]int
}

func (w *Watcher) Run(ctx context.Context) error {
	factory := informers.NewSharedInformerFactory(w.client, time.Minute)
	secretLister := factory.Core().V1().Secrets().Lister()
	ingressLister := factory.Networking().V1beta1().Ingresses().Lister()
	serviceLister := factory.Core().V1().Services().Lister()

	addBackend := func(ingressPayload *IngressPayload, backend v1beta1.IngressBackend) {

		svc, err := serviceLister.Services(ingressPayload.Ingress.Namespace).Get(backend.ServiceName)
		if err != nil {
			log.Errorf("get service %s error:%s", backend.ServiceName, err.Error())
		} else {
			m := make(map[string]int)
			for _, port := range svc.Spec.Ports {
				m[port.Name] = int(port.Port)
			}
			ingressPayload.ServicePorts[svc.Name] = m
		}
	}

	onChange := func() {
		var payload *Payload
		payload = &Payload{
			TLSCertificates: make(map[string]*tls.Certificate),
		}
		//label.Selector{}
		ingresses, err := ingressLister.List(labels.Everything())
		if err != nil {
			log.Errorf("list ingress error:%s", err.Error())
			return err
		}
		for _, ingress := range ingresses {
			ingressPayload := &IngressPayload{
				Ingress: ingress,
			}
			//default ingress backend
			if ingress.Spec.Backend != nil {
				addBackend(ingressPayload, *ingress.Spec.Backend)
			}
			for _, rule := range ingress.Spec.Rules {
				for _, path := range rule.IngressRuleValue.HTTP.Paths {
					addBackend(ingressPayload, path.Backend)
				}
			}
			payload.Ingresses = append(payload.Ingresses, ingressPayload)
			for _, itls := range ingress.Spec.TLS {
				if itls.SecretName != "" {
					secret, err := secretLister.Secrets(ingress.Namespace).Get(itls.SecretName)
					if err != nil {
						log.Errorf("get secret %s error:%s", itls.SecretName, err.Error())
						continue
					}
					cert, err := tls.X509KeyPair(secret.Data["tls.crt"], secret.Data["tls.key"])
					if err != nil {
						log.Errorf("get certificate from secret %s error:%s", secret.Name, err.Error())
						continue
					}
					payload.TLSCertificates[secret.Name] = &cert
				}
			}
		}

		w.onChange(payload)
	}

	debounced := debounce.New(time.Second)
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			debounced(onChange)
		},
		UpdateFunc: func(oldobj, newobj interface{}) {
			debounced(onChange)
		},
		DeleteFunc: func(obj interface{}) {
			debounced(onChange)
		},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		informer := factory.Core().V1().Service().Informer()
		informer.AddEventHandler(handler)
		informer.Run(ctx.Done())
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		informer := factory.Networking().V1beta1().Ingress().Informer()
		informer.AddEventHandler(handler)
		informer.Run(ctx.Done())
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		informer := factory.Core().V1().Secret().Informer()
		informer.AddEventHandler(handler)
		informer.Run(ctx.Done())
		wg.Done()
	}()
	wg.Wait()
	return nil
}
