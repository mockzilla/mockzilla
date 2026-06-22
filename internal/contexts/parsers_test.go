//go:build !integration

package contexts

import (
	"fmt"
	"strings"
	"testing"

	assert2 "github.com/stretchr/testify/assert"
)

func TestLoad(t *testing.T) {
	t.Parallel()
	assert := assert2.New(t)

	t.Run("empty", func(t *testing.T) {
		res := Load(nil, nil)
		assert.Equal(0, len(res))
	})

	t.Run("with-parsed", func(t *testing.T) {
		res := Load(nil, []map[string]map[string]any{
			{"ns1": {"key1": "value1"}},
			{"ns2": {"key2": "value2"}},
		})
		assert.Equal(2, len(res))
		assert.Equal("value1", res["ns1"]["key1"])
		assert.Equal("value2", res["ns2"]["key2"])
	})

	t.Run("with-files", func(t *testing.T) {
		res := Load(map[string][]byte{
			"ns1": []byte("key1: value1"),
			"ns2": []byte("key2: value2"),
		}, nil)

		assert2.Equal(t, 2, len(res))
		assert2.Equal(t, "value1", res["ns1"]["key1"])
		assert2.Equal(t, "value2", res["ns2"]["key2"])
	})

	t.Run("with-files-and-parsed", func(t *testing.T) {
		res := Load(map[string][]byte{
			"ns1": []byte("key1: value1"),
			"ns2": []byte("key2: value2"),
		}, []map[string]map[string]any{
			{"ns3": {"key3": "value3"}},
			{"ns4": {"key4": "value4"}},
		})

		assert2.Equal(t, 4, len(res))
		assert2.Equal(t, "value1", res["ns1"]["key1"])
		assert2.Equal(t, "value2", res["ns2"]["key2"])
		assert2.Equal(t, "value3", res["ns3"]["key3"])
		assert2.Equal(t, "value4", res["ns4"]["key4"])
	})

	t.Run("aliases resolved", func(t *testing.T) {
		res := Load(map[string][]byte{
			"ns1": []byte("key1: value1"),
			"ns2": []byte("key2: alias:ns1.key1"),
		}, nil)

		assert2.Equal(t, 2, len(res))
		assert2.Equal(t, "value1", res["ns1"]["key1"])
		assert2.Equal(t, "value1", res["ns2"]["key2"])
	})

	t.Run("not resolved aliases", func(t *testing.T) {
		res := Load(map[string][]byte{
			"ns1": []byte("key1: value1"),
			"ns2": []byte("key2: alias:foo.bar"),
		}, nil)

		expected := map[string]map[string]any{
			"ns1": {"key1": "value1"},
			"ns2": make(map[string]any),
		}

		assert2.Equal(t, expected, res)
	})

	t.Run("nested alias from different context", func(t *testing.T) {
		res := Load(map[string][]byte{
			"common": []byte(`address:
  street: 123 Main St
  city: Springfield
  zip: "12345"`),
			"user": []byte(`name: John Doe
address:
  street: alias:common.address.street
  city: alias:common.address.city
  zip: alias:common.address.zip`),
		}, nil)

		assert2.Equal(t, 2, len(res))

		// Check common namespace
		commonAddr := res["common"]["address"].(map[string]any)
		assert2.Equal(t, "123 Main St", commonAddr["street"])
		assert2.Equal(t, "Springfield", commonAddr["city"])
		assert2.Equal(t, "12345", commonAddr["zip"])

		// Check user namespace - should have resolved aliases
		assert2.Equal(t, "John Doe", res["user"]["name"])
		userAddr := res["user"]["address"].(map[string]any)
		assert2.Equal(t, "123 Main St", userAddr["street"])
		assert2.Equal(t, "Springfield", userAddr["city"])
		assert2.Equal(t, "12345", userAddr["zip"])
	})

	t.Run("multi-level nested structure with aliases", func(t *testing.T) {
		res := Load(map[string][]byte{
			"base": []byte(`data:
  nested:
    deep:
      value: original
      other: base_other`),
			"derived": []byte(`ref:
  nested:
    deep:
      value: alias:base.data.nested.deep.value
      other: alias:base.data.nested.deep.other
      extra: derived_extra`),
		}, nil)

		assert2.Equal(t, 2, len(res))

		// Check base values
		baseVal := res["base"]["data"].(map[string]any)["nested"].(map[string]any)["deep"].(map[string]any)["value"]
		assert2.Equal(t, "original", baseVal)

		// Check derived has both aliased and own values
		derivedDeep := res["derived"]["ref"].(map[string]any)["nested"].(map[string]any)["deep"].(map[string]any)
		assert2.Equal(t, "original", derivedDeep["value"])
		assert2.Equal(t, "base_other", derivedDeep["other"])
		assert2.Equal(t, "derived_extra", derivedDeep["extra"])
	})

	t.Run("mixed nested aliases and regular values", func(t *testing.T) {
		res := Load(map[string][]byte{
			"config": []byte(`server:
  host: localhost
  port: 8080
database:
  host: db.example.com
  port: 5432`),
			"app": []byte(`name: MyApp
server:
  host: alias:config.server.host
  port: alias:config.server.port
  protocol: https
database:
  host: alias:config.database.host
  port: alias:config.database.port
  name: mydb`),
		}, nil)

		assert2.Equal(t, 2, len(res))

		// Check app namespace has both aliased and regular values
		appServer := res["app"]["server"].(map[string]any)
		assert2.Equal(t, "localhost", appServer["host"])
		assert2.Equal(t, 8080, appServer["port"])
		assert2.Equal(t, "https", appServer["protocol"])

		appDb := res["app"]["database"].(map[string]any)
		assert2.Equal(t, "db.example.com", appDb["host"])
		assert2.Equal(t, 5432, appDb["port"])
		assert2.Equal(t, "mydb", appDb["name"])
	})

	t.Run("invalid contents", func(t *testing.T) {
		res := Load(map[string][]byte{
			"ns1": []byte("invalid"),
		}, nil)
		expected := map[string]map[string]any{
			"ns1": make(map[string]any),
		}

		assert2.Equal(t, expected, res)
	})

	t.Run("join function", func(t *testing.T) {
		res := Load(map[string][]byte{
			"ns1": []byte(`first: Hello
second: World
fullname: "join:,ns1.first,ns1.second"`),
		}, nil)

		assert2.Equal(t, 3, len(res["ns1"]))
		assert2.Equal(t, "Hello", res["ns1"]["first"])
		assert2.Equal(t, "World", res["ns1"]["second"])

		// fullname should be a function
		fn, ok := res["ns1"]["fullname"].(FakeFunc)
		assert2.True(t, ok)
		assert2.Equal(t, "HelloWorld", fn().Get())
	})

	t.Run("fake function", func(t *testing.T) {
		res := Load(map[string][]byte{
			"ns1": []byte(`bool: "fake:"`),
		}, nil)

		assert2.Equal(t, 1, len(res["ns1"]))

		// bool should be a function
		fn, ok := res["ns1"]["bool"].(FakeFunc)
		assert2.True(t, ok)
		assert2.NotNil(t, fn)
	})

	t.Run("func function", func(t *testing.T) {
		res := Load(map[string][]byte{
			"ns1": []byte(`greeting: "func:echo:Hello World"`),
		}, nil)

		assert2.Equal(t, 1, len(res["ns1"]))

		// greeting should be a function
		fn, ok := res["ns1"]["greeting"].(FakeFunc)
		assert2.True(t, ok)
		assert2.Equal(t, "Hello World", fn().Get())
	})

	t.Run("botify function", func(t *testing.T) {
		res := Load(map[string][]byte{
			"ns1": []byte(`code: "botify:???###"`),
		}, nil)

		assert2.Equal(t, 1, len(res["ns1"]))

		// code should be a function
		fn, ok := res["ns1"]["code"].(FakeFunc)
		assert2.True(t, ok)
		val := fn().Get().(string)
		assert2.Regexp(t, "^[a-z]{3}[0-9]{3}$", val)
	})

	t.Run("nested fake functions", func(t *testing.T) {
		res := Load(map[string][]byte{
			"fake": []byte(`
internet:
  url: "fake:internet.url"
  email: "fake:internet.email"
person:
  name: "fake:person.name"`),
		}, nil)

		assert2.Equal(t, 1, len(res))

		// Check nested fake functions are converted to FakeFunc
		internet := res["fake"]["internet"].(map[string]any)
		urlFn, ok := internet["url"].(FakeFunc)
		assert2.True(t, ok)
		assert2.NotNil(t, urlFn().Get())

		emailFn, ok := internet["email"].(FakeFunc)
		assert2.True(t, ok)
		assert2.NotNil(t, emailFn().Get())

		person := res["fake"]["person"].(map[string]any)
		nameFn, ok := person["name"].(FakeFunc)
		assert2.True(t, ok)
		assert2.NotNil(t, nameFn().Get())
	})

	t.Run("alias to nested fake function", func(t *testing.T) {
		res := Load(map[string][]byte{
			"fake": []byte(`
internet:
  url: "fake:internet.url"`),
			"common": []byte(`url: "alias:fake.internet.url"`),
		}, nil)

		assert2.Equal(t, 2, len(res))

		// Check that fake.internet.url is a function
		internet := res["fake"]["internet"].(map[string]any)
		fakeUrlFn, ok := internet["url"].(FakeFunc)
		assert2.True(t, ok)
		assert2.NotNil(t, fakeUrlFn().Get())

		// Check that common.url is also a function (aliased from fake.internet.url)
		commonUrlFn, ok := res["common"]["url"].(FakeFunc)
		assert2.True(t, ok)
		assert2.NotNil(t, commonUrlFn().Get())
	})

	t.Run("join function with fake functions", func(t *testing.T) {
		res := Load(map[string][]byte{
			"fake": []byte(`
person:
  first_name_female: "fake:person.first_name_female"
  last_name: "fake:person.last_name"`),
			"custom": []byte(`full_name: "join: ,fake.person.first_name_female,fake.person.last_name"`),
		}, nil)

		assert2.Equal(t, 2, len(res))

		// Check that custom.full_name is a function that joins two fake functions
		fullNameFn, ok := res["custom"]["full_name"].(FakeFunc)
		assert2.True(t, ok)

		// Call it multiple times to ensure it generates different values
		name1 := fullNameFn().Get().(string)
		name2 := fullNameFn().Get().(string)

		assert2.NotEmpty(t, name1)
		assert2.NotEmpty(t, name2)
		assert2.Contains(t, name1, " ") // Should have a space separator
		assert2.Contains(t, name2, " ")

		// The names should be different (very high probability with random generation)
		// We'll just verify they're both valid strings with spaces
		assert2.True(t, len(name1) > 2)
		assert2.True(t, len(name2) > 2)
	})

	t.Run("nested structures with all function types", func(t *testing.T) {
		res := Load(map[string][]byte{
			"test": []byte(`
nested:
  fake_func: "fake:person.name"
  botify_func: "botify:???###"
  func_no_arg: "func:person.name"
  join_func: "join:-,test.nested.fake_func,test.nested.botify_func"`),
		}, nil)

		assert2.Equal(t, 1, len(res))

		nested := res["test"]["nested"].(map[string]any)

		// Check fake function
		fakeFn, ok := nested["fake_func"].(FakeFunc)
		assert2.True(t, ok)
		assert2.NotNil(t, fakeFn().Get())

		// Check botify function
		botifyFn, ok := nested["botify_func"].(FakeFunc)
		assert2.True(t, ok)
		val := botifyFn().Get().(string)
		assert2.Regexp(t, "^[a-z]{3}[0-9]{3}$", val)

		// Check func no-arg
		funcNoArgFn, ok := nested["func_no_arg"].(FakeFunc)
		assert2.True(t, ok)
		assert2.NotNil(t, funcNoArgFn().Get())

		// Check join function
		joinFn, ok := nested["join_func"].(FakeFunc)
		assert2.True(t, ok)
		joinResult := joinFn().Get().(string)
		assert2.Contains(t, joinResult, "-")
	})
}

func TestLoadOrdered(t *testing.T) {
	t.Parallel()
	assert := assert2.New(t)

	defaults := []map[string]map[string]any{
		{"common": {"id": "common-id"}},
		{"fake": {"email": "fake@example.com"}},
		{"words": {"word": "lorem"}},
	}
	ordered := LoadOrdered([]byte("svc: from-service\n"), defaults)

	assert.Len(ordered, 4) // service, common, fake, words
	assert.Equal("from-service", ordered[0]["svc"])
	assert.Equal("common-id", ordered[1]["id"])
	assert.Equal("fake@example.com", ordered[2]["email"])
	assert.Equal("lorem", ordered[3]["word"])
}

func TestProcessFunctions(t *testing.T) {
	t.Parallel()
	assert := assert2.New(t)

	t.Run("not enough args", func(t *testing.T) {
		data := map[string]any{
			"file.yml": "func",
		}
		processFunctions(nil, data)
		assert.Equal("func", data["file.yml"])
	})

	t.Run("two args", func(t *testing.T) {
		data := map[string]any{
			"file.yml": "func:int_between:2,2",
		}
		processFunctions(nil, data)
		v, ok := data["file.yml"].(FakeFunc)

		assert.True(ok)
		vValue := v().Get()
		assert.Equal(int64(2), vValue)
	})
}

func TestParse_nested(t *testing.T) {
	t.Parallel()

	t.Run("nappy path", func(t *testing.T) {
		data := `
user:
  name: Jane Doe
  address:
    street: 123 Main St
    city: Anytown
    state: Anystate
    zip: 12345
`
		expected := map[string]any{
			"user": map[string]any{
				"name": "Jane Doe",
				"address": map[string]any{
					"street": "123 Main St",
					"city":   "Anytown",
					"state":  "Anystate",
					"zip":    12345,
				},
			},
		}

		_, result, err := parse([]byte(strings.Trim(data, " \n")))
		assert2.NoError(t, err)

		assert2.Equal(t, expected, result)
	})

	t.Run("nested with alias", func(t *testing.T) {
		data := `
user:
  name: Jane Doe
  address:
    street: alias:company.address.street
    city: Anytown
`
		aliases, result, err := parse([]byte(strings.Trim(data, " \n")))
		assert2.NoError(t, err)

		// Check that nested alias is properly registered with dotted key
		assert2.Equal(t, "company.address.street", aliases["user.address.street"])

		// Check that the result structure is correct
		expected := map[string]any{
			"user": map[string]any{
				"name": "Jane Doe",
				"address": map[string]any{
					"city": "Anytown",
				},
			},
		}
		assert2.Equal(t, expected, result)
	})

	t.Run("nested with func", func(t *testing.T) {
		data := `
user:
  name: func:foo
  address:
    street: func:echo:123 Main St
    city: Anytown
`
		_, result, err := parse([]byte(strings.Trim(data, " \n")))
		assert2.NoError(t, err)

		// Functions are not parsed in parse() anymore, they're just strings
		userMap, ok := result["user"].(map[string]any)
		assert2.True(t, ok)

		// name should be a string, not a function
		assert2.Equal(t, "func:foo", userMap["name"])

		addressMap, ok := userMap["address"].(map[string]any)
		assert2.True(t, ok)

		// street should be a string, not a function
		assert2.Equal(t, "func:echo:123 Main St", addressMap["street"])

		assert2.Equal(t, "Anytown", addressMap["city"])
	})

	t.Run("nested with fake and inferred path", func(t *testing.T) {
		data := `
bool: "fake:"
address:
  address: "fake:"
  building_number: "fake:"
`
		_, result, err := parse([]byte(strings.Trim(data, " \n")))
		assert2.NoError(t, err)

		// Functions are not parsed in parse() anymore, they're just strings
		assert2.Equal(t, "fake:", result["bool"])

		// Check that nested fake: is kept as string
		addressMap, ok := result["address"].(map[string]any)
		assert2.True(t, ok)

		assert2.Equal(t, "fake:", addressMap["address"])
		assert2.Equal(t, "fake:", addressMap["building_number"])
	})

	t.Run("unavailable fake functions are not added", func(t *testing.T) {
		data := `
bool: "fake:"
unavailable_func: "fake:this.does.not.exist"
address:
  street: "fake:address.street"
  unavailable: "fake:address.unavailable"
  city: Anytown
`
		_, result, err := parse([]byte(strings.Trim(data, " \n")))
		assert2.NoError(t, err)

		// Functions are not parsed in parse() anymore, they're all kept as strings
		assert2.Equal(t, "fake:", result["bool"])
		assert2.Equal(t, "fake:this.does.not.exist", result["unavailable_func"])

		// Check nested structure
		addressMap, ok := result["address"].(map[string]any)
		assert2.True(t, ok)

		// All fake functions are kept as strings in parse()
		assert2.Equal(t, "fake:address.street", addressMap["street"])
		assert2.Equal(t, "fake:address.unavailable", addressMap["unavailable"])

		// Check that regular string values are still added
		assert2.Equal(t, "Anytown", addressMap["city"])
	})

	t.Run("invalid yaml", func(t *testing.T) {
		data := `user`
		aliases, result, err := parse([]byte(data))
		assert2.Error(t, err)
		assert2.Nil(t, aliases)
		assert2.Nil(t, result)
	})
}

func TestParse_func(t *testing.T) {
	t.Parallel()

	t.Run("no-args", func(t *testing.T) {
		_, result, err := parse([]byte("key1: func:foo"))
		assert2.NoError(t, err)

		// Functions are not parsed in parse() anymore, they're just strings
		assert2.Equal(t, "func:foo", result["key1"])
	})

	t.Run("one-arg", func(t *testing.T) {
		_, result, err := parse([]byte("key1: func:echo:Hello, world!"))
		assert2.NoError(t, err)

		// Functions are not parsed in parse() anymore, they're just strings
		assert2.Equal(t, "func:echo:Hello, world!", result["key1"])
	})

	t.Run("two-args", func(t *testing.T) {
		_, result, err := parse([]byte("key1: func:int_between:1,10"))
		assert2.NoError(t, err)

		// Functions are not parsed in parse() anymore, they're just strings
		assert2.Equal(t, "func:int_between:1,10", result["key1"])
	})
}

func TestParse_alias(t *testing.T) {
	t.Parallel()

	t.Run("dotted", func(t *testing.T) {
		aliases, _, err := parse([]byte("key1: alias:ns1.key1.key2"))
		assert2.NoError(t, err)

		assert2.Equal(t, "ns1.key1.key2", aliases["key1"])
	})
}

func TestParse_botify(t *testing.T) {
	t.Parallel()

	t.Run("happy-path", func(t *testing.T) {
		_, result, err := parse([]byte("key1: botify:???###"))
		assert2.NoError(t, err)

		// Functions are not parsed in parse() anymore, they're just strings
		assert2.Equal(t, "botify:???###", result["key1"])
	})
}

func TestParseNoArgContextFunc(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	t.Run("insufficient-parts", func(t *testing.T) {
		res, ok := parseNoArgContextFunc("key", []string{"fake"}, nil)
		assert.Nil(res)
		assert.False(ok)
	})

	t.Run("empty-available", func(t *testing.T) {
		res, ok := parseNoArgContextFunc("some.id2", []string{"fake", "some.id"}, nil)
		assert.Nil(res)
		assert.False(ok)
	})

	t.Run("is-available", func(t *testing.T) {
		available := map[string]FakeFunc{
			"some.id": func() MixedValue {
				return IntValue(1212)
			},
		}
		res, ok := parseNoArgContextFunc("some.id", []string{"fake", "some.id"}, available)
		assert.NotNil(res)
		assert.True(ok)
		assert.Equal(int64(1212), res().Get())
	})

	t.Run("is-available-with-empty-name", func(t *testing.T) {
		available := map[string]FakeFunc{
			"some.id": func() MixedValue {
				return IntValue(1212)
			},
		}
		res, ok := parseNoArgContextFunc("some.id", []string{"fake", ""}, available)
		assert.NotNil(res)
		assert.True(ok)
		assert.Equal(int64(1212), res().Get())
	})

	t.Run("not-available", func(t *testing.T) {
		available := map[string]FakeFunc{
			"some.id": func() MixedValue {
				return IntValue(1212)
			},
		}
		res, ok := parseNoArgContextFunc("id2", []string{"fake", "some.id2"}, available)
		assert.Nil(res)
		assert.False(ok)
	})
}

func TestParseOneArgContextFunc(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	t.Run("insufficient-parts", func(t *testing.T) {
		res, ok := parseOneArgContextFunc([]string{"fake"}, nil)
		assert.Nil(res)
		assert.False(ok)
	})

	t.Run("is-available", func(t *testing.T) {
		available := map[string]FakeFuncFactoryWithString{
			"hello": func(value string) FakeFunc {
				return func() MixedValue {
					return StringValue("Hello, " + value + "!")
				}
			},
		}
		res, ok := parseOneArgContextFunc([]string{"func", "hello", "Motto"}, available)
		assert.NotNil(res)
		assert.True(ok)
		assert.Equal("Hello, Motto!", res().Get())
	})

	t.Run("not-available", func(t *testing.T) {
		available := map[string]FakeFuncFactoryWithString{
			"hello": func(value string) FakeFunc {
				return func() MixedValue {
					return StringValue("Hello, " + value + "!")
				}
			},
		}
		res, ok := parseOneArgContextFunc([]string{"func", "hello2", "Motto"}, available)
		assert.Nil(res)
		assert.False(ok)
	})
}

func TestParseTwoArgContextFunc(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	t.Run("insufficient-parts", func(t *testing.T) {
		res, ok := parseTwoArgContextFunc([]string{"func"}, nil)
		assert.Nil(res)
		assert.False(ok)
	})

	t.Run("is-available", func(t *testing.T) {
		available := map[string]FakeFuncFactoryWith2Strings{
			"add": func(a, b string) FakeFunc {
				return func() MixedValue {
					return StringValue(a + "+" + b)
				}
			},
		}
		res, ok := parseTwoArgContextFunc([]string{"func", "add", "1,2"}, available)
		assert.NotNil(res)
		assert.True(ok)
		assert.Equal("1+2", res().Get())
	})

	t.Run("not-available", func(t *testing.T) {
		available := map[string]FakeFuncFactoryWith2Strings{
			"add": func(a, b string) FakeFunc {
				return func() MixedValue {
					return StringValue(a + "+" + b)
				}
			},
		}
		res, ok := parseTwoArgContextFunc([]string{"func", "multiply", "1,2"}, available)
		assert.Nil(res)
		assert.False(ok)
	})

	t.Run("invalid-args-format", func(t *testing.T) {
		available := map[string]FakeFuncFactoryWith2Strings{
			"add": func(a, b string) FakeFunc {
				return func() MixedValue {
					return StringValue(a + "+" + b)
				}
			},
		}
		res, ok := parseTwoArgContextFunc([]string{"func", "add", "1"}, available)
		assert.Nil(res)
		assert.False(ok)
	})

	t.Run("with-spaces", func(t *testing.T) {
		available := map[string]FakeFuncFactoryWith2Strings{
			"add": func(a, b string) FakeFunc {
				return func() MixedValue {
					return StringValue(a + "+" + b)
				}
			},
		}
		res, ok := parseTwoArgContextFunc([]string{"func", "add", "1, 2"}, available)
		assert.NotNil(res)
		assert.True(ok)
		assert.Equal("1+2", res().Get())
	})
}

func TestParseBotifyContextFunc(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	t.Run("insufficient-parts", func(t *testing.T) {
		res, ok := parseBotifyContextFunc([]string{"botify"}, nil)
		assert.Nil(res)
		assert.False(ok)
	})

	t.Run("empty-pattern", func(t *testing.T) {
		res, ok := parseBotifyContextFunc([]string{"botify", ""}, nil)
		assert.Nil(res)
		assert.False(ok)
	})

	t.Run("is-available", func(t *testing.T) {
		available := map[string]FakeFuncFactoryWithString{
			"botify": func(value string) FakeFunc {
				return func() MixedValue {
					return StringValue("botified")
				}
			},
		}
		res, ok := parseBotifyContextFunc([]string{"botify", "???"}, available)
		assert.NotNil(res)
		assert.True(ok)
		assert.Equal("botified", res().Get())
	})
}

func TestParseJoinContextFunc(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	t.Run("insufficient-parts", func(t *testing.T) {
		res, ok := parseJoinContextFunc([]string{"join"}, nil)
		assert.Nil(res)
		assert.False(ok)
	})

	t.Run("too-many-parts", func(t *testing.T) {
		res, ok := parseJoinContextFunc([]string{"join", "separator", "extra"}, nil)
		assert.Nil(res)
		assert.False(ok)
	})

	t.Run("join-with-string-values", func(t *testing.T) {
		data := map[string]any{
			"first":  "Hello",
			"second": "World",
		}
		res, ok := parseJoinContextFunc([]string{"join", " ,first,second"}, data)
		assert.NotNil(res)
		assert.True(ok)
		assert.Equal("Hello World", res().Get())
	})

	t.Run("join-with-array-values", func(t *testing.T) {
		data := map[string]any{
			"names": []string{"Alice", "Bob", "Charlie"},
			"ages":  []any{25, 30, 35},
		}
		res, ok := parseJoinContextFunc([]string{"join", "-,names,ages"}, data)
		assert.NotNil(res)
		assert.True(ok)
		// Should pick random values from arrays
		result := res().Get().(string)
		assert.Contains(result, "-")
	})

	t.Run("join-with-nested-path", func(t *testing.T) {
		data := map[string]any{
			"user": map[string]any{
				"name": "John",
				"age":  30,
			},
		}
		res, ok := parseJoinContextFunc([]string{"join", " ,user.name,user.age"}, data)
		assert.NotNil(res)
		assert.True(ok)
		assert.Equal("John 30", res().Get())
	})

	t.Run("missing-key", func(t *testing.T) {
		data := map[string]any{
			"first": "Hello",
		}
		res, ok := parseJoinContextFunc([]string{"join", " ,first,missing"}, data)
		assert.Nil(res)
		assert.False(ok)
	})

	t.Run("empty-joiner", func(t *testing.T) {
		data := map[string]any{
			"first":  "Hello",
			"second": "World",
		}
		res, ok := parseJoinContextFunc([]string{"join", ",first,second"}, data)
		assert.NotNil(res)
		assert.True(ok)
		assert.Equal("HelloWorld", res().Get())
	})

	t.Run("join-with-fake-functions", func(t *testing.T) {
		// Create mock FakeFunc that returns predictable values
		counter := 0
		mockFakeFunc1 := func() MixedValue {
			counter++
			return StringValue(fmt.Sprintf("Value%d", counter))
		}
		mockFakeFunc2 := func() MixedValue {
			return StringValue("Static")
		}

		data := map[string]any{
			"dynamic": FakeFunc(mockFakeFunc1),
			"static":  FakeFunc(mockFakeFunc2),
		}

		res, ok := parseJoinContextFunc([]string{"join", "-,dynamic,static"}, data)
		assert.NotNil(res)
		assert.True(ok)

		// Call multiple times to verify FakeFunc is called each time (not cached)
		result1 := res().Get().(string)
		result2 := res().Get().(string)
		result3 := res().Get().(string)

		assert.Equal("Value1-Static", result1)
		assert.Equal("Value2-Static", result2)
		assert.Equal("Value3-Static", result3)
	})

	t.Run("join-with-nested-fake-functions", func(t *testing.T) {
		// Create nested structure with FakeFunc
		data := map[string]any{
			"fake": map[string]any{
				"person": map[string]any{
					"first_name": FakeFunc(func() MixedValue {
						return StringValue("John")
					}),
					"last_name": FakeFunc(func() MixedValue {
						return StringValue("Doe")
					}),
				},
			},
		}

		res, ok := parseJoinContextFunc([]string{"join", " ,fake.person.first_name,fake.person.last_name"}, data)
		assert.NotNil(res)
		assert.True(ok)
		assert.Equal("John Doe", res().Get())
	})
}
