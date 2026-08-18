# FAQ

**Why did eco join every channel on the network?**
On networks with 75 channels or fewer, eco joins everything so a newcomer immediately sees where the people are. Turn this off in Preferences under Channels, or leave specific channels with /part, Ctrl+W, or a right-click. Channels you left are not rejoined.

**What do the status dot colors mean?**
Green: connected. Orange: connecting, or reconnecting after a drop. Red: disconnected. Hover it for the details.

**Where are my passwords stored?**
In your operating system's secure store: the keyring (GNOME Keyring, KWallet) on Linux, the Credential Manager on Windows. Never in plain files when a secure store exists.

**The server's welcome text shows codes like $c04 instead of colors.**
That server has motd formatting disabled in its own configuration. The codes are literal text sent by the server; nothing is wrong with your client.

**Why does a message I sent appear instantly even on a slow server?**
eco shows your messages immediately and reconciles with the server's confirmation in the background, so a laggy server never makes typing feel broken.

**What is the line limit when I paste multiple lines?**
Whatever the server allows for one multiline message (24 lines on a default Ergo server). eco reads the limit from the server and warns before truncating.

**How do I use eco with a bouncer (ZNC, soju)?**
Add a network profile per bouncer network. For ZNC put "youruser/networkname:yourpassword" in the Server password field under Advanced. For soju set the SASL account to "youruser/networkname" with your password in Account password. Pin each profile and every bouncer network lives in your sidebar.

**Does eco work on networks other than Ergo?**
Yes, any IRC server. Modern features light up when the server supports them and eco behaves like a classic client when it does not.

**Why will eco not connect to a plain-text server?**
If Preferences has "Allow encrypted connections only" enabled, unencrypted connections are refused on purpose. Disable it or enable TLS on the profile. Servers with an STS policy are always connected over TLS.

**Does eco save my conversations?**
Yes, on this computer. eco keeps one history file per network profile, so your channels are still full of what was said in them after you restart, your unread counts pick up where you left off, and scrolling up keeps working past what the server still remembers. Nothing is ever sent anywhere: the file is on your disk and only your copy of eco reads it.

**Is the saved history encrypted?**
**No.** The history file is an ordinary database on your disk with your messages in it in plain text. Anyone who can read your files can read it: another account on a shared machine, an unencrypted backup, or anyone who ends up with the disk. eco does not encrypt it and does not pretend to. If that matters to you, use full-disk or home-directory encryption — that is the layer this problem is solved at, and eco adding its own would only give you a password to lose and a false sense of where the risk is.

**How do I stop eco saving a particular channel or person?**
Right-click it in the sidebar and choose "Stop saving history here". Nothing said in it from that moment on is written down, while everything else keeps being saved. "Erase saved history…" in the same menu deletes what is already stored for it, including its search index, and rebuilds the file so the words are gone from the disk and not merely from the tables. Both settings live with the history itself, so they survive a restart.

**How do I stop eco saving private messages entirely?**
Preferences → History → "Never save private messages". The first time a private conversation opens, eco asks you this directly rather than deciding for you.

**How long is history kept?**
Forever, unless you say otherwise. Preferences → History offers a year, 90 days, 30 days, or "only while eco is open" — which keeps history for the session so scrolling up works, and deletes it when you quit. Trimming happens when eco starts and when you change the setting.

**Where is the history file?**
Under your platform's application data directory, in `eco/archive/`, one `.db` file per network profile: `~/.local/share/eco/archive/` on Linux, `%AppData%\eco\archive\` on Windows. Deleting a file there erases that network's history; eco makes a new one next time you connect.

**Someone deleted a message. Is it still in the file?**
The message is removed from the history file and from its search index straight away, and a "message deleted" marker is left in its place so the conversation around it still reads. That happens whether or not the message was still on screen. One caveat eco would rather state than gloss: a database reclaims space lazily, so until the file is next rebuilt the deleted words can still be turned up by someone reading its raw bytes. "Erase saved history…", changing the retention setting and "only while eco is open" all rebuild it — so if you want the text off the disk now rather than eventually, right-click the conversation and erase it.
