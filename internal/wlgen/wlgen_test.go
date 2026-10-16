package wlgen

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"
)

// sample exercises every argument type and message kind the generator
// handles, in the shape of the real protocols.
const sample = `<?xml version="1.0" encoding="UTF-8"?>
<protocol name="sample">
  <interface name="xy_manager" version="3">
    <description summary="makes widgets"/>
    <request name="destroy" type="destructor"/>
    <request name="get_widget">
      <arg name="id" type="new_id" interface="xy_widget"/>
      <arg name="surface" type="object" interface="wl_surface"/>
      <arg name="namespace" type="string"/>
    </request>
    <request name="bind_any">
      <arg name="name" type="uint"/>
      <arg name="id" type="new_id"/>
    </request>
    <enum name="layer">
      <entry name="background" value="0"/>
      <entry name="overlay" value="3"/>
    </enum>
    <enum name="anchor" bitfield="true">
      <entry name="top" value="1"/>
      <entry name="bottom" value="0x2"/>
    </enum>
  </interface>
  <interface name="xy_widget" version="1">
    <request name="release" type="destructor" since="2"/>
    <request name="set_size">
      <arg name="width" type="uint"/>
      <arg name="height" type="int"/>
      <arg name="fd" type="fd"/>
      <arg name="x" type="fixed"/>
    </request>
    <event name="configure">
      <arg name="serial" type="uint"/>
      <arg name="keys" type="array"/>
      <arg name="output" type="object" interface="wl_output" allow-null="true"/>
      <arg name="interface" type="string"/>
      <arg name="keymap" type="fd"/>
      <arg name="x" type="fixed"/>
    </event>
    <event name="offer">
      <arg name="id" type="new_id" interface="xy_widget"/>
    </event>
    <event name="done" type="destructor"/>
  </interface>
</protocol>`

// decls is what a generated file declares: types (with their fields),
// methods per receiver type (with their signatures), constants, vars.
type decls struct {
	fields  map[string]map[string]string // type → field → type expression
	methods map[string]map[string]string // receiver → method → signature
	consts  map[string]string            // name → value expression
	vars    map[string]bool
	source  string
}

func generate(t *testing.T, xml string, opts Options) decls {
	t.Helper()
	p, err := Parse([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	src, err := Generate(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "gen.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("generated source does not parse: %v\n%s", err, src)
	}
	expr := func(n ast.Node) string {
		var b strings.Builder
		_ = printer.Fprint(&b, fset, n)
		return b.String()
	}
	d := decls{
		fields:  map[string]map[string]string{},
		methods: map[string]map[string]string{},
		consts:  map[string]string{},
		vars:    map[string]bool{},
		source:  string(src),
	}
	for _, decl := range f.Decls {
		switch decl := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					fields := map[string]string{}
					if st, ok := spec.Type.(*ast.StructType); ok {
						for _, field := range st.Fields.List {
							for _, name := range field.Names {
								fields[name.Name] = expr(field.Type)
							}
							if len(field.Names) == 0 {
								fields[expr(field.Type)] = "embedded"
							}
						}
					}
					d.fields[spec.Name.Name] = fields
				case *ast.ValueSpec:
					for i, name := range spec.Names {
						if decl.Tok == token.CONST {
							d.consts[name.Name] = expr(spec.Values[i])
						} else {
							d.vars[name.Name] = true
						}
					}
				}
			}
		case *ast.FuncDecl:
			if decl.Recv == nil {
				continue
			}
			recv := strings.TrimPrefix(expr(decl.Recv.List[0].Type), "*")
			if d.methods[recv] == nil {
				d.methods[recv] = map[string]string{}
			}
			d.methods[recv][decl.Name.Name] = expr(decl.Type)
		}
	}
	return d
}

func TestEachInterfaceBecomesAProxyTypeWithItsInterfaceTable(t *testing.T) {
	d := generate(t, sample, Options{Header: "sample-client-protocol.h"})
	for _, typ := range []string{"XyManager", "XyWidget"} {
		if d.fields[typ]["Proxy"] != "embedded" {
			t.Errorf("%s does not embed Proxy: %v", typ, d.fields[typ])
		}
		if !d.vars[typ+"Interface"] {
			t.Errorf("no %sInterface table", typ)
		}
	}
	for _, want := range []string{
		`#include "sample-client-protocol.h"`,
		`C.xy_manager_interface`,
		`Name: "xy_widget"`,
		`Version: 3`,
	} {
		if !strings.Contains(d.source, want) {
			t.Errorf("generated source lacks %q", want)
		}
	}
}

func TestTheCoreProtocolIncludesLibwaylandAndSkipsTheDisplay(t *testing.T) {
	core := `<protocol name="wayland">
	  <interface name="wl_display" version="1"><request name="sync"><arg name="callback" type="new_id" interface="wl_callback"/></request></interface>
	  <interface name="wl_callback" version="1"><event name="done" type="destructor"><arg name="data" type="uint"/></event></interface>
	</protocol>`
	d := generate(t, core, Options{})
	if !strings.Contains(d.source, "#include <wayland-client.h>") {
		t.Error("the core protocol does not include <wayland-client.h>")
	}
	if _, ok := d.fields["WlDisplay"]; ok {
		t.Error("wl_display is generated; the runtime owns the display")
	}
	if _, ok := d.fields["WlCallback"]; !ok {
		t.Error("wl_callback is missing")
	}
}

func TestRequestsBecomeMethodsWithTypedArguments(t *testing.T) {
	d := generate(t, sample, Options{Header: "sample-client-protocol.h"})
	for recv, want := range map[string]map[string]string{
		"XyManager": {
			"GetWidget": "func(surface *WlSurface, namespace string) *XyWidget",
			"BindAny":   "func(name uint32, iface *Interface, version uint32) Object",
			"Destroy":   "func()",
		},
		"XyWidget": {
			"SetSize": "func(width uint32, height int32, fd int, x Fixed)",
			"Release": "func()",
			// Without a destroy request, Destroy only forgets the proxy.
			"Destroy": "func()",
		},
	} {
		for method, sig := range want {
			if got := d.methods[recv][method]; got != sig {
				t.Errorf("%s.%s: %q, want %q", recv, method, got, sig)
			}
		}
	}
}

func TestEventsBecomeHandlerFieldsWithTypedArguments(t *testing.T) {
	d := generate(t, sample, Options{Header: "sample-client-protocol.h"})
	want := map[string]string{
		"OnConfigure": "func(serial uint32, keys []byte, output *WlOutput, interface_ string, keymap int, x Fixed)",
		"OnOffer":     "func(id *XyWidget)",
		"OnDone":      "func()",
	}
	for field, typ := range want {
		if got := d.fields["XyWidget"][field]; got != typ {
			t.Errorf("XyWidget.%s: %q, want %q", field, got, typ)
		}
	}
	if d.methods["XyWidget"]["dispatch"] == "" {
		t.Error("XyWidget has no dispatch method")
	}
}

func TestEnumEntriesBecomeConstantsWithTheirValues(t *testing.T) {
	d := generate(t, sample, Options{Header: "sample-client-protocol.h"})
	for name, value := range map[string]string{
		"XyManagerLayerBackground": "0",
		"XyManagerLayerOverlay":    "3",
		"XyManagerAnchorTop":       "1",
		"XyManagerAnchorBottom":    "0x2",
	} {
		if got := d.consts[name]; got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

func TestTheGeneratedFileIsMarkedGenerated(t *testing.T) {
	d := generate(t, sample, Options{Header: "sample-client-protocol.h", Source: "protocols/sample.xml"})
	first := strings.SplitN(d.source, "\n", 2)[0]
	if first != "// Code generated by wlgen from protocols/sample.xml; DO NOT EDIT." {
		t.Errorf("first line %q", first)
	}
}

func TestParseRefusesWhatTheGeneratorCannotWrap(t *testing.T) {
	for name, xml := range map[string]string{
		"array request":  `<protocol name="p"><interface name="i" version="1"><request name="r"><arg name="a" type="array"/></request></interface></protocol>`,
		"unknown type":   `<protocol name="p"><interface name="i" version="1"><event name="e"><arg name="a" type="double"/></event></interface></protocol>`,
		"untyped new_id": `<protocol name="p"><interface name="i" version="1"><event name="e"><arg name="a" type="new_id"/></event></interface></protocol>`,
		"two new_ids":    `<protocol name="p"><interface name="i" version="1"><request name="r"><arg name="a" type="new_id" interface="i"/><arg name="b" type="new_id" interface="i"/></request></interface></protocol>`,
		"no interfaces":  `<protocol name="p"></protocol>`,
		"not XML at all": `{"protocol": 1}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(xml)); err == nil {
				t.Error("parsed without error")
			}
		})
	}
}

func TestNamesBecomeGoIdentifiers(t *testing.T) {
	for in, want := range map[string]string{
		"wl_surface":          "WlSurface",
		"zwlr_layer_shell_v1": "ZwlrLayerShellV1",
		"set_size":            "SetSize",
		"90":                  "90",
		"flipped_270":         "Flipped270",
	} {
		if got := camel(in); got != want {
			t.Errorf("camel(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"app_id":    "appId",
		"interface": "interface_",
		"type":      "type_",
		"x":         "x",
	} {
		if got := lowerCamel(in); got != want {
			t.Errorf("lowerCamel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOptionsFollowTheProtocolFileName(t *testing.T) {
	for path, want := range map[string]Options{
		"protocols/wayland.xml":   {Source: "protocols/wayland.xml"},
		"protocols/xdg-shell.xml": {Header: "xdg-shell-client-protocol.h", Source: "protocols/xdg-shell.xml"},
		"viewporter.xml":          {Header: "viewporter-client-protocol.h", Source: "viewporter.xml"},
	} {
		if got := OptionsFor(path); got != want {
			t.Errorf("OptionsFor(%q) = %+v, want %+v", path, got, want)
		}
	}
}
