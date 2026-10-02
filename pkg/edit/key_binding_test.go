package edit

import (
	"runtime"
	"testing"

	"src.elv.sh/pkg/cli/term"
	"src.elv.sh/pkg/eval"
	"src.elv.sh/pkg/eval/vals"
	"src.elv.sh/pkg/must"
	"src.elv.sh/pkg/ui"
)

func TestBaseLayoutBindings(t *testing.T) {
	for _, test := range []struct {
		name    string
		primary rune
		config  string
		want    string
	}{
		{"ASCII primary wins", 'Z',
			`set edit:insert:binding[Ctrl-Z] = { set called = primary }
			 set edit:insert:binding[Ctrl-Y] = { set called = base }`, "primary"},
		{"ASCII base fallback", 'Z',
			`set edit:insert:binding[Ctrl-Y] = { set called = base }`, "base"},
		{"Unicode primary wins", 'ц',
			`set edit:insert:binding[Ctrl-ц] = { set called = primary }
			 set edit:insert:binding[Ctrl-Y] = { set called = base }`, "primary"},
		{"Unicode base fallback", 'ц',
			`set edit:insert:binding[Ctrl-Y] = { set called = base }`, "base"},
		{"base before default", 'Z',
			`set edit:insert:binding[Ctrl-Y] = { set called = base }
			 set edit:insert:binding[Default] = { set called = default }`, "base"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t, rc(`var called = ''`, test.config))
			f.TTYCtrl.Inject(term.KeyEventWithAlternates{
				KeyEvent: term.K(test.primary, ui.Ctrl),
				Keys:     [4]ui.Key{ui.K(test.primary, ui.Ctrl), {}, ui.K('Y', ui.Ctrl)},
			})
			f.TTYCtrl.Inject(term.K(ui.Enter))
			f.Wait()
			testGlobal(t, f.Evaler, "called", test.want)
		})
	}
}

func TestLayeredBindingsPreferActiveLayout(t *testing.T) {
	primary := eval.NewGoFn("primary", func() {})
	base := eval.NewGoFn("base", func() {})
	def := eval.NewGoFn("default", func() {})
	keys := []ui.Key{ui.K('Z', ui.Ctrl), ui.K('Y', ui.Ctrl)}
	upper := bindingsMap{vals.EmptyMap.Assoc(keys[1], base).Assoc(ui.DefaultKey, def)}
	lower := bindingsMap{vals.EmptyMap.Assoc(keys[0], primary)}
	if got := indexLayeredBindings(keys, upper, lower); got != primary {
		t.Errorf("selected %v, want active-layout binding", got)
	}
}

func TestRankedBindingsAcrossScopes(t *testing.T) {
	ctrlZWithBaseY := term.KeyEventWithAlternates{
		KeyEvent: term.K('Z', ui.Ctrl),
		Keys:     [4]ui.Key{ui.K('Z', ui.Ctrl), {}, ui.K('Y', ui.Ctrl)}}
	ctrlPlus := term.KeyEventWithAlternates{
		KeyEvent: term.K('+', ui.Ctrl),
		Keys:     [4]ui.Key{ui.K('=', ui.Ctrl, ui.Shift), ui.K('+', ui.Ctrl)}}
	for _, test := range []struct {
		name, config, want string
		event              term.Event
	}{
		{"global active before insert base", `set edit:insert:binding[Ctrl-Y] = { set called = base }
		 set edit:global-binding[Ctrl-Z] = { set called = primary }`, "primary", ctrlZWithBaseY},
		{"global active before insert default", `set edit:insert:binding[Default] = { set called = default }
		 set edit:global-binding[Ctrl-Z] = { set called = primary }`, "primary", ctrlZWithBaseY},
		{"insert wins at equal quality", `set edit:insert:binding[Ctrl-Z] = { set called = insert }
		 set edit:global-binding[Ctrl-Z] = { set called = global }`, "insert", ctrlZWithBaseY},
		{"explicit shifted spelling", `set edit:insert:binding[Ctrl-Shift-=] = { set called = exact }`, "exact", ctrlPlus},
		{"shifted spelling fallback", `set edit:insert:binding[Ctrl-+] = { set called = shifted }`, "shifted", ctrlPlus},
		{"global exact before insert shifted", `set edit:insert:binding[Ctrl-+] = { set called = shifted }
		 set edit:global-binding[Ctrl-Shift-=] = { set called = exact }`, "exact", ctrlPlus},
		{"Unicode shifted spelling", `set edit:insert:binding[Ctrl-Ä] = { set called = shifted }`, "shifted",
			term.KeyEventWithAlternates{
				KeyEvent: term.K('ä', ui.Ctrl, ui.Shift),
				Keys:     [4]ui.Key{ui.K('ä', ui.Ctrl, ui.Shift), ui.K('Ä', ui.Ctrl)}}},
		{"shifted active before base", `set edit:insert:binding[Ctrl-Shift-W] = { set called = base }
		 set edit:global-binding[Ctrl-Ц] = { set called = shifted }`, "shifted",
			term.KeyEventWithAlternates{
				KeyEvent: term.K('ц', ui.Ctrl, ui.Shift),
				Keys:     [4]ui.Key{ui.K('ц', ui.Ctrl, ui.Shift), ui.K('Ц', ui.Ctrl), ui.K('W', ui.Ctrl, ui.Shift)}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t, rc(`var called = ''`, test.config))
			f.TTYCtrl.Inject(test.event, term.K(ui.Enter))
			f.Wait()
			testGlobal(t, f.Evaler, "called", test.want)
		})
	}
}

func TestKittyShiftedBindings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("CSI-u decoding uses the Unix terminal reader")
	}
	for _, test := range []struct{ sequence, binding string }{
		{"\033[61:43;6u", "Ctrl-Shift-="},
		{"\033[61:43;6u", "Ctrl-+"},
		{"\033[228:196;6u", "Ctrl-Shift-ä"},
		{"\033[228:196;6u", "Ctrl-Ä"},
	} {
		t.Run(test.binding, func(t *testing.T) {
			r, w := must.Pipe()
			defer r.Close()
			defer w.Close()
			reader := term.NewReader(r)
			defer reader.Close()
			w.WriteString(test.sequence)
			event, err := reader.ReadEvent()
			if err != nil {
				t.Fatal(err)
			}
			f := setup(t, rc(`var called = $false`,
				`set edit:insert:binding[`+test.binding+`] = { set called = $true }`))
			f.TTYCtrl.Inject(event, term.K(ui.Enter))
			f.Wait()
			testGlobal(t, f.Evaler, "called", true)
		})
	}
}
