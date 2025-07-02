package surfboard

import (
	"fmt"
	"strings"

	"github.com/perfect-panel/server/pkg/adapter/proxy"
)

func buildShadowsocks(data proxy.Proxy, uuid string) string {
	ss, ok := data.Option.(proxy.Shadowsocks)
	if !ok {
		return ""
	}
	// Not supporting SIP022 AEAD-2022 Ciphers
	if strings.Contains(ss.Method, "2022") {
		return ""
	}
	addr := fmt.Sprintf("%s=ss, %s, %d", data.Name, data.Server, data.Port)
	config := []string{
		addr,
		fmt.Sprintf("encrypt-method=%s", ss.Method),
		fmt.Sprintf("password=%s", uuid),
		"tfo=true",
		"udp-relay=true",
	}
	return strings.Join(config, ",") + "\r\n"
}
