package conceptschema

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const maxWork int64 = 1_000_000
const maxSchemaBytes = 1 << 20
const maxSchemas = 128

type budget struct {
	ctx       context.Context
	remaining int64
}

func (b *budget) consume(n int64) {
	if b.ctx.Err() != nil {
		panic(&ResourceError{"evaluation_time"})
	}
	b.remaining -= n
	if b.remaining < 0 {
		panic(&ResourceError{"evaluation_work"})
	}
}

// size charges every node and scalar byte before an evaluator sees the value.
func (b *budget) size(v any, depth int, quadratic bool) int64 {
	if depth > 128 {
		panic(&ResourceError{"input_nesting"})
	}
	b.consume(1)
	n := int64(1)
	switch value := v.(type) {
	case map[string]any:
		for key, child := range value {
			b.consume(int64(len(key)))
			n += int64(len(key)) + b.size(child, depth+1, quadratic)
		}
	case []any:
		if quadratic {
			b.consume(int64(len(value)) * int64(len(value)))
		}
		for _, child := range value {
			n += b.size(child, depth+1, quadratic)
		}
	case string:
		b.consume(int64(len(value)))
		n += int64(len(value))
	case json.Number:
		number := value.String()
		if len(number) > 4096 {
			panic(&ResourceError{"numeric_digits"})
		}
		if i := strings.IndexAny(number, "eE"); i >= 0 {
			exponent, err := strconv.Atoi(number[i+1:])
			if err != nil || exponent > 10_000 || exponent < -10_000 {
				panic(&ResourceError{"numeric_scale"})
			}
		}
		b.consume(int64(len(number)))
		n += int64(len(number))
	}
	return n
}

type boundedRegexp struct {
	re     *regexp2.Regexp
	budget *budget
}

func (r *boundedRegexp) String() string { return r.re.String() }
func (r *boundedRegexp) MatchString(value string) bool {
	r.budget.consume(int64(len(value)) + 1)
	matched, err := r.re.MatchString(value)
	if err != nil {
		panic(&ResourceError{"regex_time"})
	}
	r.budget.consume(0)
	return matched
}
func (b *budget) regexp(pattern string) (jsonschema.Regexp, error) {
	b.consume(int64(len(pattern)) + 1)
	if len(pattern) > 4096 {
		return nil, &ResourceError{"regex_bytes"}
	}
	re, err := regexp2.Compile(pattern, regexp2.ECMAScript)
	if err != nil {
		return nil, fmt.Errorf("invalid regular expression")
	}
	re.MatchTimeout = 50 * time.Millisecond
	return &boundedRegexp{re, b}, nil
}

// estimate visits every possible branch for this instance before evaluation.
// Overestimation is an operational limit, never a schema violation. This bounds
// composed/reference work without changing schemas or relying on goroutine timeouts.
func (b *budget) estimate(s *jsonschema.Schema, v any, depth, references int) {
	if s == nil {
		return
	}
	if depth > 256 {
		panic(&ResourceError{"evaluation_nesting"})
	}
	if references > 64 {
		panic(&ResourceError{"reference_depth"})
	}
	n := b.size(v, 0, false)
	child := func(s *jsonschema.Schema, v any) { b.estimate(s, v, depth+1, references) }
	if s.Enum != nil {
		b.consume(n * int64(len(s.Enum.Values)))
	}
	if s.Ref != nil {
		b.estimate(s.Ref, v, depth+1, references+1)
	}
	for _, s := range []*jsonschema.Schema{s.Not, s.If, s.Then, s.Else} {
		child(s, v)
	}
	for _, list := range [][]*jsonschema.Schema{s.AllOf, s.AnyOf, s.OneOf} {
		for _, s := range list {
			child(s, v)
		}
	}
	switch value := v.(type) {
	case map[string]any:
		b.consume(int64(len(s.Required)))
		for key, item := range value {
			child(s.Properties[key], item)
			child(s.PropertyNames, key)
			for _, schema := range s.PatternProperties {
				child(schema, item)
			}
			if schema, ok := s.AdditionalProperties.(*jsonschema.Schema); ok {
				child(schema, item)
			}
			child(s.UnevaluatedProperties, item)
			child(s.DependentSchemas[key], value)
			b.consume(int64(len(s.DependentRequired[key])))
		}
	case []any:
		if s.UniqueItems {
			b.consume(n * int64(len(value)) * int64(len(value)))
		}
		for i, item := range value {
			if i < len(s.PrefixItems) {
				child(s.PrefixItems[i], item)
			}
			child(s.Items2020, item)
			child(s.Contains, item)
			child(s.UnevaluatedItems, item)
		}
	}
}

func recoverResource(err *error) {
	if p := recover(); p != nil {
		if limit, ok := p.(*ResourceError); ok {
			*err = limit
		} else {
			panic(p)
		}
	}
}
