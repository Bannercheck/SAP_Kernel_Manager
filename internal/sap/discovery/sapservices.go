package discovery

import (
	"regexp"
	"strings"
)

var (
	pfRe          = regexp.MustCompile(`pf=(\S+)`)
	sapstartsrvRe = regexp.MustCompile(`(\S+)[/\\]sapstartsrv(?:\.exe)?\b`)
)

// ParseSapservices parses /usr/sap/sapservices. Both the classic form
//
//	LD_LIBRARY_PATH=...; /usr/sap/ABC/ASCS01/exe/sapstartsrv pf=/usr/sap/ABC/SYS/profile/ABC_ASCS01_sapci -D -u abcadm
//
// and the systemd form
//
//	systemctl --no-ask-password start SAPABC_01 # sapstartsrv pf=/usr/sap/ABC/SYS/profile/ABC_ASCS01_sapci
//
// are supported.
func ParseSapservices(content string) []Instance {
	var res []Instance
	for _, line := range strings.Split(content, "\n") {
		m := pfRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		profile := m[1]
		base := profile
		if i := strings.LastIndexAny(base, `/\`); i >= 0 {
			base = base[i+1:]
		}
		parts := strings.SplitN(base, "_", 3)
		if len(parts) < 3 {
			continue
		}
		typ, nr, ok := ParseInstanceName(parts[1])
		if !ok {
			continue
		}
		in := Instance{SID: parts[0], Nr: nr, Name: parts[1], Type: typ, Host: parts[2], Profile: profile, Source: "sapservices"}
		if sm := sapstartsrvRe.FindStringSubmatch(line); sm != nil && strings.ContainsAny(sm[1], `/\`) {
			in.ExeDir = sm[1]
		} else {
			in.ExeDir = "/usr/sap/" + in.SID + "/" + in.Name + "/exe"
		}
		res = append(res, in)
	}
	return res
}
