package app

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"dosh/internal/domain"
)

const maxServicePorts = 10

var dnsLabelPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

var serviceTypes = map[string]struct{}{
	"ClusterIP":    {},
	"NodePort":     {},
	"LoadBalancer": {},
}

var serviceProtocols = map[string]struct{}{
	"TCP":  {},
	"UDP":  {},
	"SCTP": {},
}

func normalizeServiceCreateSpec(spec *domain.ServiceCreateSpec) error {
	if err := validateServiceIdentity(spec.Namespace, spec.Name); err != nil {
		return err
	}
	return normalizeServiceSpec(&spec.Type, &spec.Headless, &spec.Selector, &spec.Ports)
}

func normalizeServiceUpdateSpec(namespace, name string, update *domain.ServiceUpdateSpec) error {
	if err := validateServiceIdentity(namespace, name); err != nil {
		return err
	}
	return normalizeServiceSpec(&update.Type, &update.Headless, &update.Selector, &update.Ports)
}

func validateServiceIdentity(namespace, name string) error {
	if namespace == "" || len(namespace) > 63 || !dnsLabelPattern.MatchString(namespace) {
		return serviceValidationError("namespace", "namespace must be a valid DNS-1123 label")
	}
	if name == "" || len(name) > 63 || !dnsLabelPattern.MatchString(name) {
		return serviceValidationError("name", "name must be a valid DNS-1123 label")
	}
	return nil
}

func normalizeServiceSpec(serviceType *string, headless *bool, selector *map[string]string, ports *[]domain.ServicePortInput) error {
	*serviceType = strings.TrimSpace(*serviceType)
	if *serviceType == "" {
		*serviceType = "ClusterIP"
	}
	if _, ok := serviceTypes[*serviceType]; !ok {
		return serviceValidationError("type", "type must be ClusterIP, NodePort, or LoadBalancer")
	}
	if *headless && *serviceType != "ClusterIP" {
		return serviceValidationError("headless", "headless Services must use the ClusterIP type")
	}

	normalizedSelector := make(map[string]string, len(*selector))
	if len(*selector) == 0 {
		return serviceValidationError("selector", "at least one selector label is required")
	}
	if len(*selector) > 32 {
		return serviceValidationError("selector", "at most 32 selector labels are allowed")
	}
	for key, value := range *selector {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || len(key) > 253 {
			return serviceValidationError("selector", "selector keys must not be empty")
		}
		if len(value) > 63 {
			return serviceValidationError("selector", "selector value for %q is too long", key)
		}
		normalizedSelector[key] = value
	}
	*selector = normalizedSelector

	if len(*ports) == 0 || len(*ports) > maxServicePorts {
		return serviceValidationError("ports", "between 1 and %d ports are required", maxServicePorts)
	}
	normalizedPorts := make([]domain.ServicePortInput, 0, len(*ports))
	seenNames := make(map[string]struct{}, len(*ports))
	seenPorts := make(map[string]struct{}, len(*ports))
	for index, port := range *ports {
		port.Name = strings.TrimSpace(port.Name)
		port.Protocol = strings.ToUpper(strings.TrimSpace(port.Protocol))
		if port.Protocol == "" {
			port.Protocol = "TCP"
		}
		if _, ok := serviceProtocols[port.Protocol]; !ok {
			return serviceValidationError("ports", "port %d has an unsupported protocol", index+1)
		}
		if port.Port < 1 || port.Port > 65535 {
			return serviceValidationError("ports", "port %d must be between 1 and 65535", index+1)
		}
		port.TargetPort = strings.TrimSpace(port.TargetPort)
		if port.TargetPort == "" {
			port.TargetPort = strconv.Itoa(int(port.Port))
		}
		if numeric, err := strconv.Atoi(port.TargetPort); err == nil {
			if numeric < 1 || numeric > 65535 {
				return serviceValidationError("ports", "targetPort %d must be between 1 and 65535", index+1)
			}
		} else if len(port.TargetPort) > 15 || !dnsLabelPattern.MatchString(port.TargetPort) {
			return serviceValidationError("ports", "targetPort %d must be a port number or DNS-1123 label", index+1)
		}
		if len(*ports) > 1 && port.Name == "" {
			return serviceValidationError("ports", "port names are required when multiple ports are configured")
		}
		if port.Name != "" {
			if len(port.Name) > 15 || !dnsLabelPattern.MatchString(port.Name) {
				return serviceValidationError("ports", "port name %q is invalid", port.Name)
			}
			if _, exists := seenNames[port.Name]; exists {
				return serviceValidationError("ports", "port name %q is duplicated", port.Name)
			}
			seenNames[port.Name] = struct{}{}
		}
		portKey := fmt.Sprintf("%s/%d", port.Protocol, port.Port)
		if _, exists := seenPorts[portKey]; exists {
			return serviceValidationError("ports", "port %d/%s is duplicated", port.Port, port.Protocol)
		}
		seenPorts[portKey] = struct{}{}
		normalizedPorts = append(normalizedPorts, port)
	}
	*ports = normalizedPorts
	return nil
}

func serviceValidationError(field, format string, args ...any) error {
	return &domain.ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}
