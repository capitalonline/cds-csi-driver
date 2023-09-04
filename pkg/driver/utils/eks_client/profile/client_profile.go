package profile

type ClientProfile struct {
	HttpProfile *HttpProfile
	// Valid choices: HmacSHA1, HmacSHA256.
	// Default value is HmacSHA1.
	SignMethod      string
	UnsignedPayload bool
	// Valid choices: zh-CN, en-US.
	// Default value is zh-CN.
	Language string
}

func NewClientProfile() *ClientProfile {
	return &ClientProfile{
		HttpProfile:     NewHttpProfile(),
		SignMethod:      "HmacSHA1",
		UnsignedPayload: false,
		Language:        "zh-CN",
	}
}
func NewClientProfileWithMethod(method string) *ClientProfile {
	httpProfile := NewHttpProfile()
	httpProfile.ReqMethod = method
	return &ClientProfile{
		HttpProfile:     httpProfile,
		SignMethod:      "HmacSHA1",
		UnsignedPayload: false,
		Language:        "zh-CN",
	}
}

func (c *ClientProfile) SetMethod(method string) *ClientProfile {
	c.HttpProfile.ReqMethod = method
	return c
}
