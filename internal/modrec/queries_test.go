package modrec

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

var t0 = time.Unix(1_800_000_000, 0)

func f64(v float64) *float64 { return &v }
func intp(v int) *int        { return &v }

func TestApplyAgents(t *testing.T) {
	const example = `[
 {"id":"a1","kind":"local_agent","status":"running","type":"Explore","cost":{"usd":0.5},
  "usage":[{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":300,"cache_creation_input_tokens":5,"model":"m"}]},
 {"id":"a2","kind":"local_agent","status":"waiting","type":"general-purpose",
  "usage":[{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"model":"m"},
           {"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":1,"cache_creation_input_tokens":1,"model":"n"}]},
 {"id":"a3","kind":"local_agent","status":"pending","type":"Explore","cost":{"usd":0.25}},
 {"id":"a4","kind":"local_agent","status":"completed","type":"Plan","cost":{"usd":9}},
 {"id":"t1","kind":"in_process_teammate","status":"running","type":"teammate"},
 {"id":"t2","kind":"in_process_teammate","status":"idle","type":"teammate"},
 {"id":"bad","kind":"local_agent","status":"running","type":7},
 42, "x", null
]`
	tests := []struct {
		name string
		raw  string
		want *schema.ModAgents
	}{
		{
			name: "example",
			raw:  example,
			want: &schema.ModAgents{
				At:               t0,
				Running:          3,
				ByType:           []schema.ModTypeCount{{Type: "Explore", Count: 2}, {Type: "general-purpose", Count: 1}},
				CostUSD:          f64(0.75),
				Tokens:           440,
				TeammatesRunning: 1,
				TeammatesIdle:    1,
			},
		},
		{
			name: "type ties sort by type asc, empty type counted",
			raw:  `[{"kind":"local_agent","status":"running","type":"b"},{"kind":"local_agent","status":"running","type":"a"},{"kind":"local_agent","status":"running","type":""}]`,
			want: &schema.ModAgents{
				At:      t0,
				Running: 3,
				ByType:  []schema.ModTypeCount{{Type: "", Count: 1}, {Type: "a", Count: 1}, {Type: "b", Count: 1}},
			},
		},
		{
			name: "running local agent without cost leaves CostUSD nil",
			raw:  `[{"kind":"local_agent","status":"running","type":"x","cost":{}}]`,
			want: &schema.ModAgents{At: t0, Running: 1, ByType: []schema.ModTypeCount{{Type: "x", Count: 1}}},
		},
		{
			name: "reported zero cost is non-nil",
			raw:  `[{"kind":"local_agent","status":"running","type":"x","cost":{"usd":0}}]`,
			want: &schema.ModAgents{At: t0, Running: 1, ByType: []schema.ModTypeCount{{Type: "x", Count: 1}}, CostUSD: f64(0)},
		},
		{
			name: "empty array",
			raw:  `[]`,
			want: &schema.ModAgents{At: t0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s schema.ModSnapshot
			if !ApplyAgents(&s, t0, json.RawMessage(tt.raw)) {
				t.Fatalf("ApplyAgents returned false")
			}
			if !reflect.DeepEqual(s.Agents, tt.want) {
				t.Fatalf("Agents = %+v, want %+v", s.Agents, tt.want)
			}
		})
	}
}

func TestApplyAgentsRejects(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `"x"`, `not json`} {
		var s schema.ModSnapshot
		if ApplyAgents(&s, t0, json.RawMessage(raw)) {
			t.Errorf("ApplyAgents(%s) = true, want false", raw)
		}
		if s.Agents != nil {
			t.Errorf("ApplyAgents(%s) changed Agents to %+v", raw, s.Agents)
		}
	}
}

func TestApplyAgentsAt(t *testing.T) {
	prev := &schema.ModAgents{At: t0, Running: 9}
	s := schema.ModSnapshot{Agents: prev}

	if ApplyAgents(&s, t0.Add(-time.Second), json.RawMessage(`[]`)) {
		t.Fatalf("older at applied")
	}
	if s.Agents != prev || s.Agents.Running != 9 {
		t.Fatalf("older at changed Agents: %+v", s.Agents)
	}

	if !ApplyAgents(&s, t0, json.RawMessage(`[]`)) {
		t.Fatalf("equal at not applied")
	}
	if s.Agents == prev {
		t.Fatalf("equal at did not replace Agents")
	}
}

func TestApplyUsage(t *testing.T) {
	const example = `{"startedAt":1,"rateLimits":[],"context":{"window":200000,"tokens":5000,"breakdown":{
 "categories":[{"name":"System prompt","tokens":3000,"color":"c","isDeferred":false,"kind":"used"},7,
               {"name":"Free space","tokens":150000,"color":"c","isDeferred":false,"kind":"free"},
               {"name":"Autocompact buffer","tokens":33000,"color":"c","isDeferred":false,"kind":"buffer"}],
 "totalTokens":17000,"maxTokens":200000,"autoCompactThreshold":167000,"isAutoCompactEnabled":true,
 "gridRows":[[]],"model":"Opus","memoryFiles":[],"mcpTools":[],"agents":[]}}}`
	tests := []struct {
		name string
		raw  string
		want *schema.ModContext
	}{
		{
			name: "example",
			raw:  example,
			want: &schema.ModContext{
				At:                   t0,
				AutoCompactThreshold: intp(167000),
				TotalTokens:          17000,
				Categories: []schema.ModContextCategory{
					{Name: "System prompt", Tokens: 3000, Kind: "used"},
					{Name: "Free space", Tokens: 150000, Kind: "free"},
					{Name: "Autocompact buffer", Tokens: 33000, Kind: "buffer"},
				},
			},
		},
		{
			name: "auto-compact disabled hides threshold",
			raw:  `{"context":{"breakdown":{"totalTokens":1,"autoCompactThreshold":167000,"isAutoCompactEnabled":false}}}`,
			want: &schema.ModContext{At: t0, TotalTokens: 1},
		},
		{
			name: "auto-compact enabled without threshold",
			raw:  `{"context":{"breakdown":{"isAutoCompactEnabled":true}}}`,
			want: &schema.ModContext{At: t0},
		},
		{
			name: "auto-compact enabled with null threshold",
			raw:  `{"context":{"breakdown":{"isAutoCompactEnabled":true,"autoCompactThreshold":null}}}`,
			want: &schema.ModContext{At: t0},
		},
		{
			name: "fractional total truncates and empty categories are nil",
			raw:  `{"context":{"breakdown":{"totalTokens":17000.7,"categories":[]}}}`,
			want: &schema.ModContext{At: t0, TotalTokens: 17000},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s schema.ModSnapshot
			if !ApplyUsage(&s, t0, json.RawMessage(tt.raw)) {
				t.Fatalf("ApplyUsage returned false")
			}
			if !reflect.DeepEqual(s.Context, tt.want) {
				t.Fatalf("Context = %+v, want %+v", s.Context, tt.want)
			}
		})
	}
}

func TestApplyUsageRejects(t *testing.T) {
	for _, raw := range []string{
		`{"context":{"window":200000}}`,
		`{"context":{"breakdown":null}}`,
		`{}`,
		`[]`,
		`not json`,
	} {
		var s schema.ModSnapshot
		if ApplyUsage(&s, t0, json.RawMessage(raw)) {
			t.Errorf("ApplyUsage(%s) = true, want false", raw)
		}
		if s.Context != nil {
			t.Errorf("ApplyUsage(%s) changed Context to %+v", raw, s.Context)
		}
	}
}

func TestApplyUsageAt(t *testing.T) {
	const raw = `{"context":{"breakdown":{"totalTokens":5}}}`
	prev := &schema.ModContext{At: t0, TotalTokens: 9}
	s := schema.ModSnapshot{Context: prev}

	if ApplyUsage(&s, t0.Add(-time.Second), json.RawMessage(raw)) {
		t.Fatalf("older at applied")
	}
	if s.Context != prev || s.Context.TotalTokens != 9 {
		t.Fatalf("older at changed Context: %+v", s.Context)
	}

	if !ApplyUsage(&s, t0, json.RawMessage(raw)) {
		t.Fatalf("equal at not applied")
	}
	if s.Context == prev || s.Context.TotalTokens != 5 {
		t.Fatalf("equal at did not replace Context: %+v", s.Context)
	}
}

func TestMalformedElementsKeepOthers(t *testing.T) {
	var agents schema.ModSnapshot
	ApplyAgents(&agents, t0, json.RawMessage(`[
 {"kind":"local_agent","status":"running","type":"a","usage":[{"input_tokens":5},"bad",{"output_tokens":2}]},
 {"kind":"local_agent","status":"running","type":7},
 {"kind":"local_agent","status":"running","type":"b"}
]`))
	want := &schema.ModAgents{
		At:      t0,
		Running: 2,
		ByType:  []schema.ModTypeCount{{Type: "a", Count: 1}, {Type: "b", Count: 1}},
		Tokens:  7,
	}
	if !reflect.DeepEqual(agents.Agents, want) {
		t.Fatalf("Agents = %+v, want %+v", agents.Agents, want)
	}

	var ctx schema.ModSnapshot
	ApplyUsage(&ctx, t0, json.RawMessage(`{"context":{"breakdown":{"categories":[
 {"name":"x","tokens":1,"kind":"used"},7,
 {"name":"y","tokens":2,"kind":"free"}
]}}}`))
	wantCats := []schema.ModContextCategory{
		{Name: "x", Tokens: 1, Kind: "used"},
		{Name: "y", Tokens: 2, Kind: "free"},
	}
	if !reflect.DeepEqual(ctx.Context.Categories, wantCats) {
		t.Fatalf("Categories = %+v, want %+v", ctx.Context.Categories, wantCats)
	}
}
