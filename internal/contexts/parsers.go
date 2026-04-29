package contexts

import (
	"fmt"
	"log/slog"
	"math/rand"
	"strings"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"go.yaml.in/yaml/v4"
)

// Load processes context files and parsed contexts, resolving aliases and functions.
//
// The function operates in three phases:
//  1. Parse phase: Parses YAML files and extracts aliases and raw data
//  2. Alias resolution phase: Resolves all aliases across namespaces
//  3. function processing phase: Processes function prefixes (fake, func, botify, join)
//
// Parameters:
//   - files: Map of namespace names to YAML file contents
//   - parsed: Pre-parsed contexts to merge with file-based contexts
//
// Returns a map of namespace names to their resolved context data.
//
// Supported alias syntax:
//   - alias:namespace.key.path - References a value from another namespace
//
// Supported function prefixes:
//   - fake: - Generates fake data using available faker functions
//   - fake:path.to.func - Uses a specific faker function by path
//   - func:name - Calls a registered no-arg function
//   - func:name:arg - Calls a registered function with one argument
//   - func:name:arg1,arg2 - Calls a registered function with two arguments (e.g., func:int_between:1,10)
//   - botify:pattern - Generates random strings based on pattern (? for letter, # for digit)
//   - join:separator,ns.key1,ns.key2 - Joins values from multiple keys with separator
//
// Example:
//
//	files := map[string][]byte{
//	    "common": []byte(`
//	        host: localhost
//	        port: 8080
//	    `),
//	    "app": []byte(`
//	        server_host: alias:common.host
//	        server_port: alias:common.port
//	        code: botify:???###
//	    `),
//	}
//	result := Load(files, nil)
//	// result["app"]["server_host"] == "localhost"
//	// result["app"]["server_port"] == 8080
//	// result["app"]["code"] is a function that generates strings like "abc123"
func Load(files map[string][]byte, parsed []map[string]map[string]any) map[string]map[string]any {
	aliases := make(map[string]map[string]string)
	result := make(map[string]map[string]any)

	for _, p := range parsed {
		for ns, value := range p {
			result[ns] = value
		}
	}

	for ns, file := range files {
		aliases[ns] = make(map[string]string)
		result[ns] = make(map[string]any)

		parsedAliases, parsedResult, err := parse(file)
		if err != nil {
			continue
		}

		for key, value := range parsedAliases {
			aliases[ns][key] = value
		}
		for key, value := range parsedResult {
			result[ns][key] = value
		}
	}

	// resolve aliases first (they can refer to different namespaces)
	for ctxName, requiredAliases := range aliases {
		for ctxSourceKey, aliasTarget := range requiredAliases {
			parts := strings.Split(aliasTarget, ".")
			ns, nsPath := parts[0], strings.Join(parts[1:], ".")
			if res := types.GetValueByDottedPath(result[ns], nsPath); res != nil {
				types.SetValueByDottedPath(result[ctxName], ctxSourceKey, res)
			} else {
				slog.Error(fmt.Sprintf("context %s requires alias %s, but it's not defined", ctxName, ctxSourceKey))
			}
		}
	}

	// process all function prefixes after aliases are resolved
	// this needs to be recursive to handle nested structures like fake.internet.url
	for ns := range result {
		processFunctions(result, result[ns])
	}

	return result
}

// processFunctions recursively processes all string values and converts function prefixes to FakeFunc.
// It handles nested maps to support structures like fake.internet.url: "fake:internet.url"
// The full path is already embedded in the string value (e.g., "fake:internet.url") from the generated fake.yml
func processFunctions(allResults map[string]map[string]any, ctx map[string]any) {
	for key, value := range ctx {
		switch v := value.(type) {
		case string:
			parts := strings.Split(v, ":")
			prefix := strings.ToLower(parts[0])

			var (
				res any
				ok  bool
			)

			switch prefix {
			case "fake":
				// parts[1] contains the full path (e.g., "internet.url")
				res, ok = parseNoArgContextFunc(key, parts, ContextFunctions0Arg)
				if ok {
					ctx[key] = res
				}
			case "func":
				numArgs := len(parts) - 2
				if numArgs < 0 {
					break
				}

				// For numArgs > 0, count actual args by splitting on comma
				if numArgs > 0 {
					args := strings.Split(parts[2], ",")
					numArgs = len(args)
				}

				switch numArgs {
				case 0:
					res, ok = parseNoArgContextFunc(key, parts, ContextFunctions0Arg)
					if ok {
						ctx[key] = res
					}
				case 1:
					res, ok = parseOneArgContextFunc(parts, ContextFunctions1Arg)
					if ok {
						ctx[key] = res
					}
				case 2:
					res, ok = parseTwoArgContextFunc(parts, ContextFunctions2Arg)
					if ok {
						ctx[key] = res
					}
				}
			case "botify":
				res, ok = parseBotifyContextFunc(parts, ContextFunctions1Arg)
				if ok {
					ctx[key] = res
				}
			case "join":
				// Convert map[string]map[string]any to map[string]any for cross-namespace access
				convertedResult := make(map[string]any, len(allResults))
				for k, v := range allResults {
					convertedResult[k] = v
				}
				res, ok = parseJoinContextFunc(parts, convertedResult)
				if ok {
					ctx[key] = res
				}
			}
		case map[string]any:
			// Recursively process nested maps
			processFunctions(allResults, v)
		}
	}
}

func parse(contents []byte) (map[string]string, map[string]any, error) {
	return parseWithPath(contents, "")
}

func parseWithPath(contents []byte, pathPrefix string) (map[string]string, map[string]any, error) {
	transformed := make(map[string]any)
	if err := yaml.Unmarshal(contents, &transformed); err != nil {
		return nil, nil, err
	}

	result := make(map[string]any)
	aliases := make(map[string]string)

	for key, value := range transformed {
		// Build the full path for this key
		fullPath := key
		if pathPrefix != "" {
			fullPath = pathPrefix + "." + key
		}

		v, isString := value.(string)
		if !isString {
			// Check if it's a nested map that needs recursive parsing
			if nestedMap, isMap := value.(map[string]any); isMap {
				nestedBytes, err := yaml.Marshal(nestedMap)
				if err == nil {
					nestedAliases, nestedResult, err := parseWithPath(nestedBytes, fullPath)
					if err == nil {
						value = nestedResult
						// Merge nested aliases with prefixed keys
						for nestedKey, nestedAlias := range nestedAliases {
							aliases[key+"."+nestedKey] = nestedAlias
						}
					}
				}
			}
			// For non-map values (primitives) or if parsing failed, keep original value
			result[key] = value
			continue
		}

		// Only process aliases here, everything else goes to result as-is
		parts := strings.Split(v, ":")
		prefix := strings.ToLower(parts[0])

		if prefix == "alias" {
			alias := v[6:]
			aliases[key] = alias
			continue
		}

		// Keep all other string values in result for later processing
		result[key] = value
	}

	return aliases, result, nil
}

func parseNoArgContextFunc(key string, valueParts []string, available map[string]FakeFunc) (FakeFunc, bool) {
	if len(valueParts) < 2 || len(available) == 0 {
		return nil, false
	}
	value := valueParts[1]

	// function name can be explicitly set or inferred from key
	if value == "" {
		value = key
	}
	if fn, exists := available[value]; exists {
		return fn, true
	}
	return nil, false
}

func parseOneArgContextFunc(valueParts []string, available map[string]FakeFuncFactoryWithString) (FakeFunc, bool) {
	if len(valueParts) < 3 || len(available) == 0 {
		return nil, false
	}

	funcName := valueParts[1]
	if fn, exists := available[funcName]; exists {
		arg1 := valueParts[2]
		// call the factory function with the argument
		return fn(arg1), true
	}
	return nil, false
}

func parseTwoArgContextFunc(valueParts []string, available map[string]FakeFuncFactoryWith2Strings) (FakeFunc, bool) {
	if len(valueParts) < 3 || len(available) == 0 {
		return nil, false
	}

	funcName := valueParts[1]
	if fn, exists := available[funcName]; exists {
		// Split the third part by comma to get the two arguments
		args := strings.Split(valueParts[2], ",")
		if len(args) != 2 {
			return nil, false
		}
		arg1 := strings.TrimSpace(args[0])
		arg2 := strings.TrimSpace(args[1])
		// call the factory function with the arguments
		return fn(arg1, arg2), true
	}
	return nil, false
}

// parseBotifyContextFunc is a special case of parseOneArgContextFunc
// a shorter form for: `func:botify:pattern`
func parseBotifyContextFunc(valueParts []string, available map[string]FakeFuncFactoryWithString) (FakeFunc, bool) {
	if len(valueParts) < 2 || len(available) == 0 {
		return nil, false
	}
	return parseOneArgContextFunc([]string{"", "botify", valueParts[1]}, available)
}

func parseJoinContextFunc(fnParts []string, data map[string]any) (FakeFunc, bool) {
	if len(fnParts) != 2 {
		return nil, false
	}

	type getterFunc func() any
	var getters []getterFunc

	valueParts := strings.Split(fnParts[1], ",")
	joiner := valueParts[0]

	for _, part := range valueParts[1:] {
		// resolve part from data if it's a key
		if val := types.GetValueByDottedPath(data, part); val != nil {
			switch v := val.(type) {
			case FakeFunc:
				// If it's a FakeFunc, call it each time to get a new value
				getter := func() any {
					return v().Get()
				}
				getters = append(getters, getter)
			case []any:
				getter := func() any {
					rndIx := rand.Intn(len(v))
					return v[rndIx]
				}
				getters = append(getters, getter)
			case []string:
				getter := func() any {
					rndIx := rand.Intn(len(v))
					return v[rndIx]
				}
				getters = append(getters, getter)
			default:
				getter := func() any {
					return v
				}
				getters = append(getters, getter)
			}
		} else {
			return nil, false
		}
	}

	return func() MixedValue {
		res := make([]string, 0, len(getters))
		for _, v := range getters {
			val := v()
			res = append(res, fmt.Sprintf("%v", val))
		}
		return StringValue(strings.Join(res, joiner))
	}, true
}
