package domain

import (
	"encoding/json"
	"time"
)

// SysProvider 代表大模型官方厂商（系统内置字典）
type SysProvider struct {
	ProviderID         string `json:"provider_id"`
	ProviderName       string `json:"provider_name"`
	APIProtocol        string `json:"api_protocol"`
	DefaultConcurrency int    `json:"default_concurrency"`
	DefaultTimeoutSec  int    `json:"default_timeout_sec"`
}

// SysAccessEndpoint 代表某厂商的一种具体接入方式（端点）
type SysAccessEndpoint struct {
	EndpointID               string          `json:"endpoint_id"`
	ProviderID               string          `json:"provider_id"`
	DisplayName              string          `json:"display_name"`
	APIProtocol              string          `json:"api_protocol"`
	DefaultBaseURL           string          `json:"default_base_url"`
	AuthType                 string          `json:"auth_type"` // e.g., bearer, header, adc
	AuthHeader               string          `json:"auth_header"`
	RequiredCredentialFields json.RawMessage `json:"required_credential_fields"` // JSON array of field names
	DisplayOrder             int             `json:"display_order"`
}

// AuthType constants defining the system-level authentication methods.
const (
	AuthTypeNone     = "none"
	AuthTypeBearer   = "bearer"
	AuthTypeHeader   = "header"
	AuthTypeQuery    = "query"
	AuthTypeADC      = "adc"
	AuthTypeAWSSigV4 = "aws_sigv4"
)

// UserProvider 代表用户实例化的渠道（通道）
type UserProvider struct {
	ID               int             `json:"id"`
	Name             string          `json:"name"`
	ProviderID       string          `json:"provider_id"`
	AuthCredentials  json.RawMessage `json:"auth_credentials"` // JSON key-value pairs
	Priority         int             `json:"priority"`
	Weight           int             `json:"weight"`
	ConcurrencyLimit int             `json:"concurrency_limit"`
	MinIntervalSec   int             `json:"min_interval_sec"`
	TimeoutSec       int             `json:"timeout_sec"`
	RetryTimes       int             `json:"retry_times"`
	Status           int             `json:"status"` // 1: 健康, 0: 禁用, -1: 熔断
	Balance          float64         `json:"balance"`
	LimitPercent     float64         `json:"limit_percent"`
	UsedAmount       float64         `json:"used_amount"`
	ValidFrom        string          `json:"valid_from"`
	ValidTo          string          `json:"valid_to"`
	CreatedAt        time.Time       `json:"created_at"`

	// 以下字段用于内存态状态控制，非数据库字段
	CurrentConcurrent int `json:"-"`

	Endpoints []UserAccessEndpoint `json:"endpoints"`
}

// UserAccessEndpoint 代表用户实例化的渠道的具体协议端点
type UserAccessEndpoint struct {
	ID             int    `json:"id"`
	UserProviderID int    `json:"user_provider_id"`
	SysEndpointID  string `json:"sys_endpoint_id"`
	IsEnabled      bool   `json:"is_enabled"`
	CustomBaseURL  string `json:"custom_base_url"`
}
