package formenc

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncode(t *testing.T) {
	tests := []struct {
		name     string
		data     any
		encoding map[string]FieldEncoding
		want     url.Values
	}{
		{
			name: "scalars",
			data: map[string]any{"name": "John", "age": 30, "ok": true},
			want: url.Values{"name": {"John"}, "age": {"30"}, "ok": {"true"}},
		},
		{
			name: "large integer keeps its digits",
			data: map[string]any{"big": uint64(18446744073709551615)},
			want: url.Values{"big": {"18446744073709551615"}},
		},
		{
			name: "list adds one value per item",
			data: map[string]any{"tags": []any{"a", "b"}},
			want: url.Values{"tags": {"a", "b"}},
		},
		{
			name: "form explode object",
			data: map[string]any{"o": map[string]any{"b": "2", "a": "1"}},
			want: url.Values{"o.a": {"1"}, "o.b": {"2"}},
		},
		{
			name:     "form without explode joins pairs",
			data:     map[string]any{"o": map[string]any{"b": "2", "a": "1"}},
			encoding: map[string]FieldEncoding{"o": {Style: "form", Explode: boolPtr(false)}},
			want:     url.Values{"o": {"a,1,b,2"}},
		},
		{
			name:     "explode is ignored without the form style",
			data:     map[string]any{"o": map[string]any{"a": "1"}},
			encoding: map[string]FieldEncoding{"o": {Explode: boolPtr(false)}},
			want:     url.Values{"o.a": {"1"}},
		},
		{
			name: "deepObject uses bracket keys",
			data: map[string]any{
				"flow": map[string]any{
					"update": map[string]any{
						"sub":   "sub_1",
						"items": []any{map[string]any{"id": "i1", "qty": 1}, map[string]any{"id": "i2"}},
					},
				},
				"plain": "x",
			},
			encoding: map[string]FieldEncoding{"flow": {Style: "deepObject"}},
			want: url.Values{
				"flow[update][sub]":           {"sub_1"},
				"flow[update][items][0][id]":  {"i1"},
				"flow[update][items][0][qty]": {"1"},
				"flow[update][items][1][id]":  {"i2"},
				"plain":                       {"x"},
			},
		},
		{
			name:     "deepObject list",
			data:     map[string]any{"expand": []any{"a", "b"}},
			encoding: map[string]FieldEncoding{"expand": {Style: "deepObject"}},
			want:     url.Values{"expand[0]": {"a"}, "expand[1]": {"b"}},
		},
		{
			name:     "encoding of another field is ignored",
			data:     map[string]any{"a": map[string]any{"b": "1"}},
			encoding: map[string]FieldEncoding{"z": {Style: "deepObject"}},
			want:     url.Values{"a.b": {"1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Encode(tt.data, tt.encoding)
			require.NoError(t, err)

			parsed, err := url.ParseQuery(got)
			require.NoError(t, err)
			assert.Equal(t, tt.want, parsed)
		})
	}
}

func TestEncode_Errors(t *testing.T) {
	tests := []struct {
		name string
		data any
	}{
		{name: "not an object", data: "text"},
		{name: "list", data: []any{"a"}},
		{name: "unmarshalable", data: make(chan int)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Encode(tt.data, nil)
			assert.Error(t, err)
		})
	}
}

func TestToJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "flat values are typed",
			body: "name=John&age=30&ok=true&off=false&price=9.5&note=hello",
			want: `{"name":"John","age":30,"ok":true,"off":false,"price":9.5,"note":"hello"}`,
		},
		{
			name: "nested object",
			body: "order%5Bpayment%5D%5Bcurrency%5D=usd&order%5Bpayment%5D%5Bamount%5D=100",
			want: `{"order":{"payment":{"currency":"usd","amount":100}}}`,
		},
		{
			name: "array of objects",
			body: "items[0][id]=a&items[0][qty]=2&items[1][id]=b",
			want: `{"items":[{"id":"a","qty":2},{"id":"b"}]}`,
		},
		{
			name: "deep path through objects and arrays",
			body: "a[b][0][c]=x&a[b][0][d]=y&a[b][1][c]=z",
			want: `{"a":{"b":[{"c":"x","d":"y"},{"c":"z"}]}}`,
		},
		{
			name: "simple array with index",
			body: "expand[0]=customer&expand[1]=invoice",
			want: `{"expand":["customer","invoice"]}`,
		},
		{
			name: "array of scalars below an object",
			body: "o[tags][0]=a&o[tags][1]=b",
			want: `{"o":{"tags":["a","b"]}}`,
		},
		{
			name: "nested arrays",
			body: "m[0][0]=1&m[0][1]=2&m[1][0]=3",
			want: `{"m":[[1,2],[3]]}`,
		},
		{
			name: "empty brackets with repeated keys",
			body: "ids[]=1&ids[]=2",
			want: `{"ids":[1,2]}`,
		},
		{
			name: "repeated keys become an array",
			body: "tag=a&tag=b&n=1&n=2",
			want: `{"tag":["a","b"],"n":[1,2]}`,
		},
		{
			name: "sparse index pads with null",
			body: "a[2]=x",
			want: `{"a":[null,null,"x"]}`,
		},
		{
			name: "huge index is an object key",
			body: "a[99999999999]=x",
			want: `{"a":{"99999999999":"x"}}`,
		},
		{
			name: "negative index is an object key",
			body: "a[-1]=x",
			want: `{"a":{"-1":"x"}}`,
		},
		{
			name: "later key replaces a scalar with a container",
			body: "a=1&a[b]=2",
			want: `{"a":{"b":2}}`,
		},
		{
			name: "phone number stays a string",
			body: "phone=%2B15551234567&other=555-1234",
			want: `{"phone":"+15551234567","other":"555-1234"}`,
		},
		{
			name: "formatted values stay strings",
			body: "a=1+2&b=%281%29&c=+",
			want: `{"a":"1 2","b":"(1)","c":" "}`,
		},
		{
			name: "leading zero stays a string",
			body: "zip=01234&zero=0&dec=0.5&neg=-7",
			want: `{"zip":"01234","zero":0,"dec":0.5,"neg":-7}`,
		},
		{
			name: "numbers that are not plain integers or decimals",
			body: "a=1e5&b=1.2.3&c=12345678901234567890&d=",
			want: `{"a":"1e5","b":"1.2.3","c":"12345678901234567890","d":""}`,
		},
		{
			name: "empty body",
			body: "",
			want: `{}`,
		},
		{
			name: "brackets only",
			body: "[]=x",
			want: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToJSON([]byte(tt.body))
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

func TestToJSON_InvalidBody(t *testing.T) {
	_, err := ToJSON([]byte("a=1;b=2"))
	assert.Error(t, err)
}

func TestToJSON_RoundTrip(t *testing.T) {
	data := map[string]any{
		"name":  "x",
		"items": []any{map[string]any{"id": "a", "qty": 2}},
		"tags":  []any{"p", "q"},
	}
	enc := map[string]FieldEncoding{"items": {Style: "deepObject"}, "tags": {Style: "deepObject"}}

	body, err := Encode(data, enc)
	require.NoError(t, err)

	got, err := ToJSON([]byte(body))
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"x","items":[{"id":"a","qty":2}],"tags":["p","q"]}`, string(got))
}

func boolPtr(b bool) *bool {
	return &b
}
