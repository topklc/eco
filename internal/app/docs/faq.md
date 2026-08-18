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
