package service

import (
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Service(namespace, name string, opts ...ServiceOption) corev1.Service {
	service := corev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{},
		},
	}
	for _, o := range opts {
		o.ApplyToService(&service)
	}
	return service
}

type ServiceOption interface {
	ApplyToService(service *corev1.Service)
}

type serviceOptionFunc func(service *corev1.Service)

func (f serviceOptionFunc) ApplyToService(service *corev1.Service) {
	f(service)
}

func WithPort(name string, port, nodePort int32, protocol corev1.Protocol) ServiceOption {
	return serviceOptionFunc(func(service *corev1.Service) {
		service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{
			Name:     name,
			Port:     port,
			NodePort: nodePort,
			Protocol: protocol,
		})
	})
}

func WithServiceType(_type corev1.ServiceType) ServiceOption {
	return serviceOptionFunc(func(service *corev1.Service) {
		service.Spec.Type = _type
	})
}
