package app

import (
	"encoding/json"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/zalando/go-keyring"
)

const keyringService = "eco"

func keyringKey(n Network) string     { return n.Server + "|" + n.SASLUser }
func keyringPassKey(n Network) string { return n.Server + "|PASS" }

func keyringGet(key string) (string, bool) {
	if pw, err := keyring.Get(keyringService, key); err == nil {
		return pw, true
	}
	return "", false
}

// Network is a saved server + identity profile.
type Network struct {
	Name       string `json:"name"`
	Server     string `json:"server"`
	Port       string `json:"port"`
	TLS        bool   `json:"tls"`
	ServerPass string `json:"server_pass"`
	Nick       string `json:"nick"`
	SASLUser   string `json:"sasl_user"`
	SASLPass   string `json:"sasl_pass"`
	SASLMech   string `json:"sasl_mech"`
	Channels   string `json:"channels"`
	Pinned     bool   `json:"pinned"`
}

const networksPrefKey = "networks_v1"

// loadNetworkMeta parses the profile list WITHOUT touching the keyring.
// Hot paths (sidebar refresh, row menus) need only names and pin flags;
// keyring roundtrips are synchronous DBus and belong at connect time only.
func loadNetworkMeta(p fyne.Preferences) []Network {
	var nets []Network
	if raw := p.String(networksPrefKey); raw != "" {
		if json.Unmarshal([]byte(raw), &nets) == nil && len(nets) > 0 {
			return nets
		}
	}
	return []Network{
		{Name: "Ergo Testnet", Server: "testnet.ergo.chat", Port: "6697", TLS: true,
			Nick: "newbie" + randSuffix()},
		{Name: "Libera.Chat", Server: "irc.libera.chat", Port: "6697", TLS: true,
			Nick: "newbie" + randSuffix(), Channels: ""},
	}
}

func loadNetworks(p fyne.Preferences) []Network {
	nets := loadNetworkMeta(p)
	for i := range nets {
		if nets[i].SASLPass == "" && nets[i].SASLUser != "" {
			if pw, ok := keyringGet(keyringKey(nets[i])); ok {
				nets[i].SASLPass = pw
			}
		}
		if nets[i].ServerPass == "" {
			if pw, ok := keyringGet(keyringPassKey(nets[i])); ok {
				nets[i].ServerPass = pw
			}
		}
	}
	return nets
}

// dropKeyringFor removes a profile's stored secrets, used when the profile
// is deleted or its identity (server or account) changes so the entries
// would otherwise be orphaned forever.
func dropKeyringFor(n Network) {
	if n.SASLUser != "" {
		keyring.Delete(keyringService, keyringKey(n))
	}
	keyring.Delete(keyringService, keyringPassKey(n))
}

// saveNetworks persists profiles to preferences; SASL passwords go to the
// system keyring instead of disk. If no keyring is available (no Secret
// Service on the session bus), the password stays in the JSON as before.
func saveNetworks(p fyne.Preferences, nets []Network) {
	clean := make([]Network, len(nets))
	copy(clean, nets)
	for i := range clean {
		if clean[i].SASLUser != "" && clean[i].SASLPass != "" {
			if err := keyring.Set(keyringService, keyringKey(clean[i]), clean[i].SASLPass); err == nil {
				clean[i].SASLPass = ""
			}
		}
		if clean[i].ServerPass != "" {
			if err := keyring.Set(keyringService, keyringPassKey(clean[i]), clean[i].ServerPass); err == nil {
				clean[i].ServerPass = ""
			}
		}
	}
	raw, _ := json.Marshal(clean)
	p.SetString(networksPrefKey, string(raw))
}

// ShowNetworkManager displays the server/identity manager. Selecting a
// network and pressing Connect saves everything and (re)connects.
func (u *UI) ShowNetworkManager() { u.showNetworkManagerFor("") }

// showNetworkManagerFor opens the manager with the named profile selected.
func (u *UI) showNetworkManagerFor(profile string) {
	p := u.app.Preferences()
	nets := loadNetworks(p)
	selected := 0
	for i, n := range nets {
		if profile != "" && n.Name == profile {
			selected = i
			break
		}
	}

	name := widget.NewEntry()
	server := widget.NewEntry()
	port := widget.NewEntry()
	useTLS := widget.NewCheck("encrypted (recommended)", nil)
	serverPass := widget.NewPasswordEntry()
	serverPass.SetPlaceHolder("connection password for bouncers (PASS), rarely needed")
	nick := widget.NewEntry()
	channels := widget.NewEntry()
	channels.SetPlaceHolder("#chan1, #chan2 (joined automatically)")
	saslUser := widget.NewEntry()
	saslUser.SetPlaceHolder("defaults to your nickname")
	saslPass := widget.NewPasswordEntry()
	saslPass.SetPlaceHolder("password of your registered account, if any")
	saslMech := widget.NewSelect([]string{"PLAIN", "SCRAM-SHA-256"}, nil)
	pinned := widget.NewCheck("Pin to the sidebar", nil)

	loadForm := func(n Network) {
		name.SetText(n.Name)
		server.SetText(n.Server)
		port.SetText(n.Port)
		useTLS.SetChecked(n.TLS)
		serverPass.SetText(n.ServerPass)
		nick.SetText(n.Nick)
		channels.SetText(n.Channels)
		saslUser.SetText(n.SASLUser)
		saslPass.SetText(n.SASLPass)
		if n.SASLMech == "" {
			saslMech.SetSelected("PLAIN")
		} else {
			saslMech.SetSelected(n.SASLMech)
		}
		pinned.SetChecked(n.Pinned)
	}
	formToNet := func() Network {
		return Network{Name: name.Text, Server: server.Text, Port: port.Text,
			TLS: useTLS.Checked, ServerPass: serverPass.Text,
			Nick: nick.Text, Channels: channels.Text,
			SASLUser: saslUser.Text, SASLPass: saslPass.Text,
			SASLMech: saslMech.Selected, Pinned: pinned.Checked}
	}

	var list *widget.List
	list = widget.NewList(
		func() int { return len(nets) },
		func() fyne.CanvasObject { return widget.NewLabel("network name") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < len(nets) {
				o.(*widget.Label).SetText(nets[i].Name)
			}
		},
	)
	list.OnSelected = func(i widget.ListItemID) {
		if i < len(nets) {
			selected = i
			loadForm(nets[i])
		}
	}

	saveCurrent := func() {
		if selected >= 0 && selected < len(nets) {
			old := nets[selected]
			nets[selected] = formToNet()
			if nets[selected].Name == "" {
				nets[selected].Name = nets[selected].Server
			}
			// a changed server or account changes the keyring key; drop the
			// old entries so renames do not orphan secrets forever
			if old.Server != nets[selected].Server || old.SASLUser != nets[selected].SASLUser {
				dropKeyringFor(old)
			}
			saveNetworks(p, nets)
			list.Refresh()
		}
	}

	addBtn := widget.NewButton("Add", func() {
		nets = append(nets, Network{Name: "New network", Port: "6697", TLS: true,
			Nick: "newbie" + randSuffix()})
		saveNetworks(p, nets)
		list.Refresh()
		list.Select(len(nets) - 1)
	})
	delBtn := widget.NewButton("Delete", func() {
		if len(nets) <= 1 {
			return // always keep one
		}
		dropKeyringFor(nets[selected])
		nets = append(nets[:selected], nets[selected+1:]...)
		if selected >= len(nets) {
			selected = len(nets) - 1
		}
		saveNetworks(p, nets)
		list.Refresh()
		list.Select(selected)
		// Select on an unchanged index does not refire OnSelected, which
		// left the deleted profile's data in the form; saving then would
		// overwrite the neighbor that slid into this slot
		loadForm(nets[selected])
	})
	saveBtn := widget.NewButton("Save", saveCurrent)

	// the essentials up front; the remaining protocol knobs live under Advanced
	essentials := widget.NewForm(
		widget.NewFormItem("Network", name),
		widget.NewFormItem("Server", server),
		widget.NewFormItem("Nickname", nick),
		widget.NewFormItem("Account password", saslPass),
		widget.NewFormItem("", pinned),
	)

	advanced := widget.NewAccordion(
		widget.NewAccordionItem("Advanced", widget.NewForm(
			widget.NewFormItem("Port", port),
			widget.NewFormItem("TLS", useTLS),
			widget.NewFormItem("Server password (PASS)", serverPass),
			widget.NewFormItem("SASL account", saslUser),
			widget.NewFormItem("SASL mechanism", saslMech),
			widget.NewFormItem("Auto-join", channels),
		)),
	)

	form := container.NewVScroll(container.NewVBox(essentials, advanced))

	left := container.NewBorder(
		widget.NewLabelWithStyle("Networks", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		container.NewGridWithColumns(2, addBtn, delBtn),
		nil, nil, list)
	// fixed layout: narrow profile list, form fills the rest; a draggable
	// divider inside a fixed-size dialog was only an accident waiting
	listWidth := canvas.NewRectangle(color.Transparent)
	listWidth.SetMinSize(fyne.NewSize(190, 0))
	body := container.NewBorder(nil, nil,
		container.NewStack(listWidth, left), nil,
		container.NewBorder(nil, saveBtn, nil, nil, form))

	d := dialog.NewCustomConfirm("Servers & Identities", "Connect", "Cancel",
		container.NewGridWrap(u.dialogSize(640, 380), body),
		func(ok bool) {
			if !ok {
				return // Cancel just closes the dialog
			}
			saveCurrent()
			u.connectTo(nets[selected])
		}, u.win)
	u.trackDialog(d)
	d.Show()
	list.Select(selected)
}

// connectTo tears down any existing session and connects to the network.
func (u *UI) connectTo(n Network) {
	cfg := Config{
		Server: strings.TrimSpace(n.Server), Port: strings.TrimSpace(n.Port),
		TLS: n.TLS, ServerPass: n.ServerPass,
		Nick: strings.TrimSpace(n.Nick), Channels: n.Channels,
		SASLUser: n.SASLUser, SASLPass: n.SASLPass, SASLMech: n.SASLMech,
	}

	// "Account password" with no explicit account means the nickname is
	// the account, which is what Ergo and most networks expect
	if cfg.SASLPass != "" && cfg.SASLUser == "" {
		cfg.SASLUser = cfg.Nick
	}

	ws := isWebSocketURL(cfg.Server)

	// a remembered STS policy always wins: this host must use TLS
	if !ws {
		if pol, ok := stsLookup(u.app.Preferences(), cfg.Server); ok && !cfg.TLS {
			cfg.TLS = true
			if pol.Port != "" {
				cfg.Port = pol.Port
			}
		}
	}
	// the TLS-only preference refuses plaintext outright; wss counts as
	// encrypted, plain ws does not
	encrypted := cfg.TLS || strings.HasPrefix(cfg.Server, "wss://")
	if u.app.Preferences().BoolWithFallback("tlsonly", false) && !encrypted {
		dialog.ShowInformation("TLS required",
			"This profile is unencrypted, but Preferences is set to allow encrypted connections only.\nEnable TLS (or use a wss:// address), or turn off the TLS-only setting.", u.win)
		return
	}

	encryptedFinal := cfg.TLS || strings.HasPrefix(cfg.Server, "wss://")
	u.app.Preferences().SetString("lastnetwork", n.Name)
	u.startSession(cfg, n.Name)
	if !encryptedFinal && (cfg.SASLPass != "" || cfg.ServerPass != "") {
		u.sess.status(serverBuffer, "WARNING: this connection is unencrypted and your password will be sent in cleartext. Enable TLS in the profile's Advanced section.")
	}
}

// startSession connects with a concrete Config. Multiple sessions may run
// at once; reconnecting to a network you are already on replaces that
// session.
func (u *UI) startSession(cfg Config, title string) {
	// a fresh connection to an already-open network replaces it
	for _, s := range u.sessions {
		if s.Name == title {
			u.closeSession(s)
			break
		}
	}

	u.networkName = title
	u.status.set(dotOrange, "Connecting to "+title)
	u.welcome.Hide()
	u.sidePane.Show()
	u.memberPane.Show()
	u.buffersBtn.Enable()
	u.channelsBtn.Enable()
	u.peopleBtn.Enable()
	u.centerBox.Refresh()
	u.root.Refresh()

	var sess *Session
	sess = NewSession(cfg, u.callbacksFor(func() *Session { return sess }))
	sess.Name = title
	u.sessions = append(u.sessions, sess)
	u.sess = sess
	u.refreshBuffers()
	u.selectRow(sess, serverBuffer)
	sess.Connect()

	u.win.SetTitle("eco: " + title)
	u.win.SetCloseIntercept(func() {
		for _, s := range u.sessions {
			s.Quit()
		}
		u.win.Close()
	})
}

// autojoinHas reports whether ch is in the profile's autojoin list.
func autojoinHas(p fyne.Preferences, profile, ch string) bool {
	for _, n := range loadNetworkMeta(p) {
		if n.Name != profile {
			continue
		}
		for _, c := range strings.Split(n.Channels, ",") {
			if strings.EqualFold(strings.TrimSpace(c), ch) {
				return true
			}
		}
	}
	return false
}

// autojoinToggle adds or removes ch from the profile's autojoin list and
// returns the new membership state.
func autojoinToggle(p fyne.Preferences, profile, ch string) bool {
	nets := loadNetworkMeta(p)
	added := false
	for i := range nets {
		if nets[i].Name != profile {
			continue
		}
		var keep []string
		found := false
		for _, c := range strings.Split(nets[i].Channels, ",") {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if strings.EqualFold(c, ch) {
				found = true
				continue
			}
			keep = append(keep, c)
		}
		if !found {
			keep = append(keep, ch)
			added = true
		}
		nets[i].Channels = strings.Join(keep, ", ")
		saveNetworks(p, nets)
		break
	}
	return added
}

// setPinned flips a profile's sidebar pin.
func setPinned(p fyne.Preferences, profile string, pinned bool) {
	nets := loadNetworkMeta(p)
	for i := range nets {
		if nets[i].Name == profile {
			nets[i].Pinned = pinned
			saveNetworks(p, nets)
			return
		}
	}
}
