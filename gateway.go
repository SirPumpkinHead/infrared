package infrared

import (
	"github.com/rs/zerolog"
	"io"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

// Gateway is a data structure that holds all gates.
// A gateway is managing all proxies by dynamically forwarding
// incoming connections to their corresponding proxy.
type Gateway struct {
	gates   map[string]*Gate
	wg      *sync.WaitGroup
	running bool

	logger        zerolog.Logger
	loggerOutputs []io.Writer
}

// NewGateway creates a new gateway that
// uses the default log.Logger from the zerolog/log package
func NewGateway() Gateway {
	return Gateway{
		gates:         map[string]*Gate{},
		wg:            &sync.WaitGroup{},
		running:       false,
		logger:        log.Logger,
		loggerOutputs: []io.Writer{},
	}
}

func (gateway *Gateway) AddLoggerOutput(w io.Writer) {
	gateway.loggerOutputs = append(gateway.loggerOutputs, w)
	gateway.logger = gateway.logger.Output(io.MultiWriter(gateway.loggerOutputs...))

	for _, gate := range gateway.gates {
		gate.AddLoggerOutput(w)
	}
}

// overrideLogger overrides its own logger and the logger of all child gates
// Note that each gate updates all their proxies.
func (gateway *Gateway) overrideLogger(logger zerolog.Logger) zerolog.Logger {
	gateway.logger = logger.Output(io.MultiWriter(gateway.loggerOutputs...))

	for _, gate := range gateway.gates {
		gate.overrideLogger(logger)
	}

	return gateway.logger
}

// AddGate manually adds the given gate to the gateway for automatic management.
// The gate's logger will be updated through the overrideLogger method.
func (gateway *Gateway) AddGate(gate *Gate) error {
	if _, ok := gateway.gates[gate.listenTo]; ok {
		return ErrGateSignatureAlreadyRegistered
	}

	gate.AddLoggerOutput(io.MultiWriter(gateway.loggerOutputs...))
	gate.overrideLogger(gateway.logger)
	gateway.gates[gate.listenTo] = gate

	gateway.logger.Debug().
		Str("gate", gate.listenTo).
		Msg("Added gate to gateway")

	if !gateway.running {
		return nil
	}

	gateway.wg.Add(1)
	go func() {
		if err := gate.ListenAndServe(); err != nil {
			gateway.logger.Err(err)
		}

		delete(gateway.gates, gate.listenTo)
		gateway.wg.Done()
	}()

	return nil
}

// RemoveGate closes the gate and then removes it from the gateway.
// If the gate does not exist, RemoveGate is a no-op.
func (gateway *Gateway) RemoveGate(addr string) {
	gate, ok := gateway.gates[addr]
	if !ok {
		return
	}

	gate.Close()
	delete(gateway.gates, addr)
}

// AddProxyByViper adds a proxy by its viper configuration.
// This enables the ability to watch the config file to update
// the proxy accordingly to changes.
func (gateway *Gateway) AddProxyByViper(vpr *viper.Viper) (*Proxy, error) {
	cfg, err := LoadProxyConfig(vpr)
	if err != nil {
		return nil, err
	}

	proxy, err := NewProxy(cfg)
	if err != nil {
		return nil, err
	}

	if err := gateway.AddProxy(proxy); err != nil {
		return proxy, err
	}

	vpr.WatchConfig()
	vpr.OnConfigChange(gateway.onConfigChange(proxy, vpr))
	return proxy, nil
}

func (gateway *Gateway) AddProxy(proxy *Proxy) error {
	gate, ok := gateway.gates[proxy.listenTo.Read()]
	if ok {
		return gate.AddProxy(proxy)
	}

	gate, err := NewGate(proxy.listenTo.Read())
	if err != nil {
		return err
	}

	if err := gateway.AddGate(gate); err != nil {
		return err
	}

	if err := gate.AddProxy(proxy); err != nil {
		return err
	}

	return nil
}

// RemoveProxy closes the proxy and then removes it from it's gate.
// If the proxy does not exist, RemoveProxy is a no-op.
func (gateway *Gateway) RemoveProxy(addr, domainName string) {
	gate, ok := gateway.gates[addr]
	if !ok {
		return
	}

	gate.RemoveProxy(domainName)
}

// ListenAndServe starts all gates
func (gateway *Gateway) ListenAndServe() error {
	gateway.logger.Info().Msgf("Starting gateway")

	if len(gateway.gates) <= 0 {
		return ErrNoGateInGateway
	}

	gateway.running = true

	for _, gate := range gateway.gates {
		loopGate := *gate
		gateway.wg.Add(1)
		go func() {
			if err := loopGate.ListenAndServe(); err != nil {
				gateway.logger.Err(err)
			}
			delete(gateway.gates, loopGate.listenTo)
			gateway.wg.Done()
		}()
	}

	gateway.wg.Wait()
	gateway.running = false
	return nil
}

// Close closes all gates
func (gateway *Gateway) Close() {
	for _, gate := range gateway.gates {
		gate.Close()
	}
}

func (gateway *Gateway) onConfigChange(proxy *Proxy, vpr *viper.Viper) func(fsnotify.Event) {
	return func(in fsnotify.Event) {
		if in.Op != fsnotify.Write {
			return
		}

		logger := gateway.logger.With().Str("path", in.Name).Logger()
		logger.Info().Msg("Configuration changed")

		cfg, err := LoadProxyConfig(vpr)
		if err != nil {
			logger.Err(err).Msg("Failed to load configuration")
			return
		}

		if err := gateway.UpdateProxy(proxy, cfg); err != nil {
			logger.Err(err)
		}
	}
}

func (gateway *Gateway) UpdateProxy(proxy *Proxy, cfg ProxyConfig) error {
	if cfg.ListenTo == proxy.listenTo.Read() {
		gate, ok := gateway.gates[proxy.listenTo.Read()]
		if !ok {
			return ErrGateDoesNotExist
		}

		return gate.UpdateProxy(proxy, cfg)
	}

	gate, ok := gateway.gates[cfg.ListenTo]
	if !ok {
		oldAddr := proxy.listenTo.Read()
		oldDomainName := proxy.domainName.Read()

		if err := proxy.updateConfig(cfg); err != nil {
			return err
		}

		if err := gateway.AddProxy(proxy); err != nil {
			return err
		}

		gateway.RemoveProxy(oldAddr, oldDomainName)
		return nil
	}

	oldDomainName := proxy.domainName.Read()

	if err := proxy.updateConfig(cfg); err != nil {
		return err
	}

	if err := gate.AddProxy(proxy); err != nil {
		return err
	}

	gate.RemoveProxy(oldDomainName)
	return nil
}
