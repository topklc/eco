package app

import (
	"crypto/sha256"
	"encoding/hex"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/toprakkilic/eco/internal/archive"
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/irc/transport"
)

// ShowNetworkManager displays the server/identity manager. Selecting a
// network and pressing Connect saves everything and (re)connects.
func (u *UI) ShowNetworkManager() { u.showNetworkManagerFor("") }

// showNetworkManagerFor opens the manager with the named profile selected.
func (u *UI) showNetworkManagerFor(profile string) {
	p := u.app.Preferences()
	nets := config.Load(p)
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

	loadForm := func(n config.Network) {
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
	formToNet := func() config.Network {
		return config.Network{Name: name.Text, Server: server.Text, Port: port.Text,
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
				config.DropSecrets(old)
			}
			if warn := config.Save(p, nets); warn != "" {
				dialog.ShowInformation("Password not saved", warn, u.win)
			}
			list.Refresh()
		}
	}

	addBtn := widget.NewButton("Add", func() {
		nets = append(nets, config.Network{Name: "New network", Port: "6697", TLS: true,
			Nick: "newbie" + randSuffix()})
		config.Save(p, nets)
		list.Refresh()
		list.Select(len(nets) - 1)
	})
	delBtn := widget.NewButton("Delete", func() {
		if len(nets) <= 1 {
			return // always keep one
		}
		config.DropSecrets(nets[selected])
		nets = append(nets[:selected], nets[selected+1:]...)
		if selected >= len(nets) {
			selected = len(nets) - 1
		}
		config.Save(p, nets)
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
		widget.NewFormItem("config.Network", name),
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

// archiveID turns a profile name into a filename the archive will accept. The
// name is the user's, so it can contain anything at all; what comes out here is
// a stable slug of it plus a short digest, because two profiles called "IRC"
// and "irc" are two networks and must not share one history file.
func archiveID(profile string) string {
	sum := sha256.Sum256([]byte(profile))
	var b strings.Builder
	for _, r := range profile {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
		if b.Len() >= 24 {
			break
		}
	}
	return b.String() + "-" + hex.EncodeToString(sum[:4])
}

// connectTo tears down any existing session and connects to the network.
func (u *UI) connectTo(n config.Network) {
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

	ws := transport.IsWebSocketURL(cfg.Server)

	// a remembered STS policy always wins: this host must use TLS
	if !ws {
		if pol, ok := config.STSLookup(u.app.Preferences(), cfg.Server); ok && !cfg.TLS {
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
		u.session().status(serverBuffer, "WARNING: this connection is unencrypted and your password will be sent in cleartext. Enable TLS in the profile's Advanced section.")
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
	// One archive per network profile (blueprint §5.4). The path is derived
	// from the profile name, so a reconnect — and a restart — come back to the
	// same file; a name eco cannot make a filename out of gets no archive
	// rather than somebody else's.
	if cfg.Archive == "" {
		if path, err := archive.DefaultPath(archiveID(title)); err == nil {
			cfg.Archive = path
		}
	}

	u.status.set(dotConnecting, "Connecting to "+title)
	u.welcome.Hide()
	u.setRailVisible(true)
	u.buffersBtn.Enable()
	u.channelsBtn.Enable()
	u.peopleBtn.Enable()
	u.readerBtn.Enable()
	u.centerBox.Refresh()
	u.root.Refresh()

	sess := NewSession(cfg, u.dispatch)
	sess.Name = title
	u.sessions = append(u.sessions, sess)
	// The profile name is what scopes drafts and counts, so it has to be
	// registered before anything is shown under it.
	u.vm.AddSession(sess.ID(), title)
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
