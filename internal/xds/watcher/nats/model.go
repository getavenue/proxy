package nats

type GatewayConfig struct {
	EnvoyConfigHash string `json:"envoy_config_hash"`
	EnvoyConfig     string `json:"envoy_config"`
	EnvoyPort       int    `json:"envoy_port"`
	NginxConfig     string `json:"nginx_config"`
}

type ProxyConfig struct {
	GatewayConfigs map[string]GatewayConfig `json:"gateway_configs"`
}

type GatewayState struct {
	EnvoyConfigHash string `json:"envoy_config_hash"`
	EnvoyPort       int    `json:"envoy_port"`
}

type ProxyState struct {
	GatewayStates map[string]GatewayState `json:"gateway_states"`
	ProxyVersion  string                  `json:"proxy_version"`
	EnvoyVersion  string                  `json:"envoy_version"`
}
