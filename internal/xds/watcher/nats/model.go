package nats

type GatewayConfig struct {
	EnvoyConfigHash string `json:"envoy_config_hash"`
	EnvoyConfig     string `json:"envoy_config"`
}

type ProxyConfig struct {
	GatewayConfigs map[string]GatewayConfig `json:"gateway_configs"`
}

type ProxyState struct {
	GatewayStates map[string]string `json:"gateway_states"`
}
