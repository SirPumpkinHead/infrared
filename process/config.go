package process

type Config struct {
	DNSServer     string
	ContainerName string
	Portainer     struct {
		Address    string
		EndpointID string
		Username   string
		Password   string
	}
}

func (cfg Config) hasPortainerConfig() bool {
	if cfg.Portainer.Address == "" {
		return false
	}

	if cfg.Portainer.EndpointID == "" {
		return false
	}

	if cfg.Portainer.Username == "" {
		return false
	}

	if cfg.Portainer.Password == "" {
		return false
	}

	return true
}
