# Contexts

Contexts provide a way to control the values generated for API responses and requests. 
They allow you to define static values, dynamic fake data, and reusable patterns that are applied during content generation.

## Overview

Contexts are organized in YAML files which act as namespaces or collections of context data.
File names typically correspond to the service or domain name (e.g., `payments.yml`, `petstore.yml`).

On the filesystem, contexts are stored with the `.yml` extension in the `contexts` directory.
For example: `contexts/payments.yml`.

**Important:** Only individual primitive properties are replaced during content generation.
You cannot substitute a property with an object or a list.

## How Context Replacement Works

The context system operates in three phases:

1. **Parse phase**: YAML files are parsed and aliases are extracted
2. **Alias resolution phase**: All aliases are resolved across namespaces
3. **Function processing phase**: Function prefixes (`fake:`, `func:`, `botify:`, `join:`) are processed

When generating content, the system looks up property names as-is (no case conversion) in the loaded contexts and replaces values accordingly.

### Default Contexts

Each distribution ships with default contexts that are automatically loaded:

| Context | Description |
|---------|-------------|
| `common` | Common patterns like `_id$`, `_email$` for suffix matching |
| `fake` | Fake data generators from the faker library |
| `words` | Common nouns, adjectives, and verbs for realistic data |

## Context Structure

Inside a context file, provide data that corresponds to your schema properties.

**Example OpenAPI schema:**
```yaml
Pet:
  type: object
  properties:
    id:
      type: string
      format: uuid
    name:
      type: string
    tag:
      type: string
```

**Context file (`petstore.yml`):**
```yaml
id: 123e4567-e89b-12d3-a456-426614174000
name: "doggie"
tag: "dog"
```

**Generated JSON response:**
```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "name": "doggie",
  "tag": "dog"
}
```

### Matching Behavior

**Root-level primitives** (strings, numbers, booleans, arrays, functions) match the field name at any depth in the generated response:

```yaml
status: ["active", "inactive"]  # Matches any field named "status" regardless of nesting
email: "fake:internet.email"    # Matches any field named "email" at any level
```

**Nested objects** use suffix matching - they only match when the response path ends with the context path:

```yaml
data:
  foo: "replace-data.foo"
```

This matches `user.address.data.foo` because the path ends with `data.foo`, but does NOT match `user.foo` (no `data` parent).

**Priority:** More specific (longer) suffix matches win over root-level matches:

```yaml
status: ["global-a", "global-b"]       # Least specific: matches "status" anywhere
order:
  status: ["pending", "shipped"]        # More specific: wins when path ends with "order.status"
```

For a field at path `order.status`, the nested `order.status` context wins over the root-level `status`.

### Nested Properties

For schemas with nested objects, use nested YAML structure with keys matching your schema property names exactly:

```yaml
Pet:
  id: 123e4567-e89b-12d3-a456-426614174000
  name: "doggie"
  tag: "dog"
ownerPerson:
  id: 1
  name: "Jane Doe"
```

**Note:** Context keys must match your schema property names exactly - there is no automatic case conversion. If your OpenAPI schema uses `camelCase`, your context keys should also be `camelCase`.

### Arrays

Arrays are transparent in context matching. The context describes the schema structure, not the runtime shape. Array items share the same schema, so the replacement applies to every generated element:

```yaml
user:
  addresses:
    city: "fake:address.city"
    street: "fake:address.street_address"
```

If `addresses` is an array of objects, each generated item gets its `city` and `street` replaced. The context path `user.addresses.city` matches regardless of how many items are produced.

## Context Functions

Context functions allow dynamic value generation. All functions use a prefix syntax: `prefix:arguments`.

### `fake:` - Fake Data Generation

Generates random values using the [jaswdr/faker](https://github.com/jaswdr/faker) library.

**Syntax:** `fake:path.to.function`

```yaml
Pet:
  id: "fake:uuid.v4"
  name: "fake:pet.name"
  tag: "fake:gamer.tag"
ownerPerson:
  id: "fake:u_int8"
  name: "fake:person.name"
  email: "fake:internet.email"
```

All available fake functions are listed in the [fake.yml](https://github.com/mockzilla/mockzilla/blob/master/resources/contexts/fake.yml) file.

**Common fake functions:**
- `fake:uuid.v4` - UUID v4
- `fake:person.name` - Full person name
- `fake:person.first_name` - First name
- `fake:internet.email` - Email address
- `fake:internet.url` - URL
- `fake:u_int8`, `fake:u_int16`, `fake:u_int32` - Unsigned integers
- `fake:phone.number` - Phone number

### `alias:` - Cross-Context References

References values from other contexts or namespaces.

**Syntax:** `alias:namespace.dotted.path`

```yaml title="petstore.yml"
id: "fake:uuid.v4"
name: "fake:pet.name"
ownerId: "alias:person.id"
ownerName: "alias:person.name"
```

```yaml title="person.yml"
id: "fake:u_int8"
name: "fake:person.name"
petId: "alias:petstore.id"
```

If an alias doesn't resolve to a valid target, it remains as-is in the output, making issues easy to spot.

### `func:` - Custom Functions

Calls registered functions with optional arguments.

**Syntax variants:**
- `func:name` - No arguments
- `func:name:arg` - One argument
- `func:name:arg1,arg2` - Two arguments

**Available functions:**

| Function      | Arguments | Description |
|---------------|-----------|-------------|
| `botify`      | pattern | Generate string from pattern (see below) |
| `echo`        | value | Return the value as-is |
| `int_between` | min,max | Random int between min and max |

**Example:**
```yaml
totalItems: "func:int_between:1,100"
code: "func:echo:FIXED_CODE"
```

### `botify:` - Pattern-Based Generation

Generates random strings based on a pattern. This is a shorthand for `func:botify:pattern`.

**Pattern characters:**
- `?` - Random letter (a-z)
- `#` - Random digit (0-9)

**Syntax:** `botify:pattern`

```yaml
password: "botify:???###"      # e.g., "abc123"
code: "botify:??-####"         # e.g., "xy-5678"
serial: "botify:???-???-###"   # e.g., "abc-def-123"
```

### `join:` - Value Concatenation

Joins values from multiple context keys with a separator.

**Syntax:** `join:separator,namespace.key1,namespace.key2,...`

```yaml
expression: "join:-,words.adjectives,words.nouns"  # e.g., "active-account"
full_name: "join: ,person.first_name,person.last_name"
```

## Pattern Matching with Dynamic Keys

Context keys can use regex patterns to match multiple property names.

### Suffix Matching

Keys ending with `$` match properties that end with that pattern:

```yaml
(_id|Id)$: "alias:fake.u_int8"           # Matches: user_id, userId, pet_id, petId
(_email|Email)$: "alias:fake.internet.email" # Matches: user_email, userEmail
(_count|Count)$: "alias:fake.u_int8"         # Matches: item_count, itemCount
```

Since there is no automatic case conversion, use alternation patterns like `(_id|Id)$` to match both `snake_case` and `camelCase` field names.

### Prefix Matching

Keys starting with `^` match properties that start with that pattern:

```yaml
(^total_|^total[A-Z]): "func:int_between:1,100"   # Matches: total_items, totalItems
```

### Wildcard Matching

Use `*` to match any property name (converted to `.*` regex):

```yaml
"*": "alias:words.expression"  # Fallback for any unmatched property
```

## Predefined Value Lists

Use a list of values to randomly select from predefined options:

```yaml
name: ["Jane", "John", "Alice", "Bob"]

# Or multi-line format:
status:
  - "pending"
  - "active"
  - "completed"
  - "cancelled"
```

A random value is picked from the list each time.

## Area-Specific Contexts

Different contexts can be applied to specific areas (path parameters, headers, etc.):

```yaml
in-path:
  petId: "alias:fake.u_int8"
  (id|Id)$: "alias:fake.u_int8"

in-header:
  x-pet-name: "alias:fake.pet.name"
  x-request-id: "alias:fake.uuid.v4"
```

Area-specific contexts take precedence over default context replacements.

### Request/Response Areas

Compound area keys allow targeting request or response generation independently. This is useful when the same field needs different values depending on direction:

```yaml
id: 42                        # default for everything
in-request:
  id: "abc-123"               # request body/query only
in-response:
  id: 100                     # response body only
in-request-header:
  authorization: "Bearer tok" # request headers only
in-response-header:
  x-request-id: "fixed"      # response headers only
```

**Priority chain (most specific wins):**

```
in-request-header > in-header > in-request > root
in-response-header > in-header > in-response > root
```

Available compound areas:

| Area | Applies to |
|------|-----------|
| `in-request` | Request body and query parameters |
| `in-response` | Response body |
| `in-request-header` | Request headers only |
| `in-response-header` | Response headers only |

### Prefix Configuration

The `in-` prefix can be changed in `config.yml`:
```yaml
app:
  contextAreaPrefix: "in-"
```

## Service Configuration

Context files are wired to services through configuration. There's no automatic mapping based on filenames.

```yaml title="config.yml"
services:
  petstore:
    contexts:
      - fake: pet        # Use only 'pet' section from fake.yml
      - fake: people     # Use only 'people' section from fake.yml
      - person:          # Use entire person.yml context
```

Replacement is applied in the order of definition. If no configuration is provided for a service, default contexts are used.

## Resolution Order

When generating values, contexts are checked in the following order. The first match wins:

1. **User context** - provided via the UI editor or `X-Mz-Context` HTTP header (base64-encoded JSON)
2. **Service context** - from the service's `context.yml` file
3. **Common context** - built-in patterns like `(_id|Id)$`, `email`, etc.
4. **Fake context** - faker library generators
5. **Words context** - common nouns, adjectives, verbs for fallback data

User context overrides service context, which overrides defaults. This applies to both request and response generation.

## Per-Request Context via Header

Context replacements can be passed with any HTTP request using the `X-Mz-Context` header. The value should be base64-encoded JSON:

```
X-Mz-Context: eyJuYW1lIjoiZm9vIiwiaWQiOjExfQ==
```

This is useful for:
- The UI context editor (sent automatically)
- CI pipelines testing specific values
- Programmatic response generation with custom data

All context functions (`func:`, `fake:`, `alias:`, `botify:`, `join:`) are supported in the header value - they are processed the same way as context YAML files.

## Using in Fixed Responses

> **⚠️ Work in Progress:** Context replacement in fixed/static responses using `{placeholder}` syntax is currently not implemented. Static responses defined via `x-static-response` are returned as-is without placeholder substitution.

For now, if you need dynamic values in responses, use schema-based generation with contexts rather than static response files.
