package relay

import "github.com/bluenviron/gortmplib/pkg/amf0"

func isMetadata(body []byte) bool {
	a, err := amf0.Unmarshal(body)
	if err != nil || len(a) == 0 {
		return false
	}
	if name, ok := a[0].(string); ok {
		if name == "onMetaData" {
			return true
		}
		if name == "@setDataFrame" && len(a) > 1 {
			v, ok := a[1].(string)
			return ok && v == "onMetaData"
		}
	}
	return false
}
