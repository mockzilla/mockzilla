package schema

// MinResponseCodes buckets HTTP status codes into 2xx/3xx/4xx/5xx and
// returns the lowest declared code in each bucket. A zero return for
// any bucket means no code in that range was declared.
func MinResponseCodes(codes []int) (min2xx, min3xx, min4xx, min5xx int) {
	for _, code := range codes {
		switch {
		case code >= 200 && code < 300:
			if min2xx == 0 || code < min2xx {
				min2xx = code
			}
		case code >= 300 && code < 400:
			if min3xx == 0 || code < min3xx {
				min3xx = code
			}
		case code >= 400 && code < 500:
			if min4xx == 0 || code < min4xx {
				min4xx = code
			}
		case code >= 500:
			if min5xx == 0 || code < min5xx {
				min5xx = code
			}
		}
	}
	return
}
