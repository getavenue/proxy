package nats

type GatewayConfig struct {
	GatewayName     string `json:"gateway_name"`
	EnvoyConfigHash string `json:"envoy_config_hash"`
	EnvoyConfig     string `json:"envoy_config"`
}

type ProxyConfig struct {
	GatewayConfigs []GatewayConfig `json:"gateway_configs"`
}

type GatewayState struct {
	GatewayName     string `json:"gateway_name"`
	EnvoyConfigHash string `json:"envoy_config_hash"`
}

type ProxyState struct {
	GatewayStates []GatewayState `json:"gateway_states"`
}
