# Getting started with eco

Welcome. This walkthrough takes you from a fresh install to being
comfortable in eco day to day. If IRC itself is new to you, the
**IRC Basics** page explains the ideas behind everything here.

## Your first connection

Start eco and you will see the leaf logo with a single button:
**Connect to a server**. Click it (or open the Control Center with the
settings icon at the top left and pick Servers & Identities).

The dialog that opens is your list of saved networks on the left and a
form on the right. Two profiles are seeded for you: the **Ergo Testnet**
(a friendly public sandbox, great for a first try) and **Libera.Chat**
(the largest open source network). To try IRC right now, select Ergo
Testnet, change the nickname if you like, and press **Connect**.

To add your own network, press **Add** and fill in:

* **Network**: any name you like, this is what the sidebar shows
* **Server**: the address, like `irc.example.com`
* **Nickname**: the name people will see
* **Account password**: only if you already registered an account there,
  otherwise leave it empty

Everything else lives under **Advanced** and the defaults are right for
almost everyone: TLS encryption on, port 6697. The **Pin to the
sidebar** checkbox keeps this network visible in the sidebar even when
you are not connected, so reconnecting is one click.

After you connect, eco scans the network's channel list. On small
networks (75 channels or fewer) it joins all of them so you can
immediately see where people are. On big networks it opens the channel
browser instead so you can pick.

## Reading the screen

**The sidebar** on the left is a tree. Each network you are connected to
is a root entry, with its channels and private conversations indented
beneath it. Click anything to view it. Bold entries with a number like
`#chat  (3)` have unread messages, and an entry shown in orange means
someone mentioned your name there. Right-click any entry for quick
actions: leave a channel, close a conversation, mark as read, or
disconnect a whole network.

**The top bar**, from left to right:

* the **list icon** hides or shows the sidebar
* the **settings icon** opens the Control Center, the hub for everything
* the current channel's name and topic
* the **status dot**: green means connected, orange means connecting or
  reconnecting, red means disconnected. Hover it for details
* the **copy icon** switches to plaintext mode, a selectable view of the
  conversation for copying text (click again to return to colors)
* the **person icon** hides or shows the member list

**The member list** on the right shows everyone in the current channel,
sorted by rank. Symbols before a name mean channel status: `@` marks an
operator, `+` marks voice. Click any name to open a private
conversation with that person.

**The message area** in the middle is the conversation. Each person
gets their own stable color. When your name is mentioned, the message
is highlighted and you get a desktop notification. New messages scroll
into view automatically, but if you scroll up to read older ones, eco
leaves you alone until you come back down.

## Talking

Type in the box at the bottom and press **Enter**. That is genuinely
all there is to it.

Some comforts as you go:

* **Shift+Enter** starts a new line inside one message. The box grows as
  you type, and a counter shows how many lines the server allows
* **Tab** completes nicknames: type a few letters, press Tab, press it
  again to cycle through matches
* **Up and Down** recall messages you sent earlier
* Anything starting with `/` is a command. `/help` opens the built-in
  guide. You never need commands for everyday chatting, every common
  action has a button or menu
* Each conversation keeps its own unsent draft, so switching channels
  mid-sentence never sends text to the wrong place

## Finding, joining, and creating channels

Open the Control Center and pick **Channels**. Start typing: the list
filters live across names and topics, sorted by how many people are in
each channel. Press **Enter** to join the top match, or click any row.

If what you typed does not exist yet, a green **Create** button appears
and Enter creates the channel with you in it. Naming it `#something` is
the convention, but eco adds the `#` for you if you forget.

## Private conversations

Click a name in the member list, or type `/msg somenick hello`. The
conversation appears in the sidebar under its network like a channel.
Close it any time with a right-click or **Ctrl+W**.

## Registering an account

Registration attaches your nickname to a password so nobody else can
use it, and unlocks extras on modern servers like message history that
follows you between devices. It is worth doing on any network you plan
to return to.

Type `/register yourpassword` while connected. If the server wants
email verification it will say so, then `/verify code` finishes the
job. Afterwards, put that password into the network profile's
**Account password** field and eco logs you in automatically every time
you connect.

## Notifications

By default eco raises a desktop notification with a sound when someone
mentions your name or messages you privately. Your own messages and
old history never trigger notifications. You can tune all of this
(including notifying for every message, or going fully silent) in
Preferences under **Notifications**.

## Preferences

Control Center, then **Preferences**. Settings are grouped:

* **Appearance**: light or dark theme, font size, your nick color,
  custom background color or image
* **Chat**: per-person nick colors, timestamps, hiding join and part
  noise
* **Channels**: the automatic channel scan and join-all behavior
* **Notifications**: popups, the ping sound, notify-on-every-message
* **Interface**: toolbar tooltips
* **Security**: refusing unencrypted connections entirely

Everything is remembered between launches.

## More than one network

Connect to a second network and it appears as another root in the
sidebar, with its own channels beneath it. Everything is kept separate
per network: unread counts, drafts, highlights. **Ctrl+PageUp** and
**Ctrl+PageDown** step through every conversation across all networks,
**Alt+1** through **Alt+9** jump directly, and **Alt+A** jumps to
whatever has unread messages.

Pinned networks stay in the sidebar when disconnected, shown as
`name (offline)`, and a click reconnects them.

## When something looks wrong

The **server log** (click a network's root entry in the sidebar) shows
everything the server tells eco, including errors. The **FAQ** page
covers the common surprises. And the status dot never lies: if it is
not green, the network it refers to is not connected.
