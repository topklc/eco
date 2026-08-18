# IRC basics

IRC (Internet Relay Chat) has been running since 1988, which makes it
the oldest chat system still in daily use. If you come from Discord or
Slack, most of it will feel familiar, because they borrowed heavily
from IRC. This page explains the ideas and the vocabulary.

## Networks, servers, and channels

A **network** is a community of servers acting as one chat system, like
Libera.Chat or a friend's private Ergo server. You connect to a network
by pointing eco at one of its **servers**. For most networks today
that distinction barely matters: one address, one network.

Inside a network are **channels**, the rooms where conversation
happens. Channel names start with `#`, like `#chat` or `#linux`.
Anyone can join a public channel, and creating a new one is usually as
simple as joining a name that does not exist yet, which instantly makes
you its operator.

Coming from Discord: a network is roughly a Discord server, and
channels are channels. The difference is that IRC networks are
independent worlds. Your nickname, account, and channels on one network
do not exist on another.

## Nicknames and accounts

Your **nickname** is how people see you, and on IRC it is your
identity. Unlike Discord there is no global account behind it by
default: on a classic server, whoever connects with a nickname first
simply has it.

That is why **registration** exists. Registering attaches a password to
your nickname on that network, so it belongs to you. Once registered,
you log in automatically each time (eco does this for you via a
mechanism called **SASL** once your password is in the network
profile). On modern servers, registration also unlocks conveniences
like your message history following you between devices.

Register early on any network you care about. It takes one command and
ten seconds, and the Getting Started page shows how.

## Operators and those @ symbols

In the member list you will see symbols in front of some names:

* `~` marks the channel **founder**
* `@` marks a channel **operator** (an "op"), who can change the topic,
  set channel rules, and remove troublemakers
* `%` marks a **half operator**, a junior moderator
* `+` marks **voice**, which matters in moderated channels where only
  voiced people may speak

These are per-channel roles, not network-wide ranks. The person who is
an op in `#linux` is a regular user everywhere else. Separately, the
network itself has staff called **IRC operators** who deal with
network-level problems; you will rarely interact with them.

## Message history, or why the room looks empty

On classic IRC, the server relays messages and remembers nothing. You
receive only what is said while you are connected, which is why old
IRC guides talk about leaving a client running all the time, and why
**bouncers** (relay services that stay connected for you) exist.

Modern servers like Ergo changed this. They store history and replay
it, so when you join a channel eco automatically shows you the recent
conversation, and with a registered account your history can follow
you across devices. On networks without this, eco still works, you
just start from the moment you joined, like walking into a room.

## NickServ and ChanServ

On many networks you will meet **services**: helpful robots operated
by the network itself.

**NickServ** manages nicknames and accounts: registration, passwords,
recovering a nickname someone else is squatting. **ChanServ** manages
channel ownership, so channels keep their operators and settings even
when everyone is offline. Some networks add others, like **HostServ**
for vanity addresses.

You talk to them with short commands (eco has shortcuts: `/ns`, `/cs`,
`/hs`), and they answer in a private conversation. A message from
NickServ right after you connect is normal, it is usually just
confirming you are logged in.

## Culture

IRC has manners that differ from modern platforms, and knowing them
makes you instantly at home:

* **Idling is normal.** People stay in channels for days without
  speaking. Silence is not awkward, it is the default state. Ask your
  question and wait, even hours; someone will answer when they see it
* **Do not ask to ask.** Instead of "can anyone help with Go?", just
  ask the actual question, with details. This is the most quoted rule
  in all of IRC
* **No read receipts, no typing pressure.** Nobody knows if you saw a
  message. Respond when you respond
* Formatting is light: bold and colors exist and eco renders them, but
  walls of colored text are considered noise
* Each channel has its own personality and often a `/topic` stating the
  rules; a quick read before diving in never hurts

## A translation guide from Discord

* A Discord server is roughly an IRC **network** (though IRC networks
  are usually much bigger and community-run)
* Channels are **channels**, same word, same idea
* DMs are **private messages** or queries
* Roles like moderator map to **channel operator** status, the `@`
* Discord's @mention is just typing someone's **nickname** in a
  message; eco highlights it and notifies them the same way
* There is no equivalent of a Discord account that spans everything.
  Each network stands alone, with its own registration
* Pinned messages, emoji reactions, and threads mostly do not exist.
  Conversation is one linear stream, and people like it that way

## Why people still choose IRC

No company owns it. Networks are run by communities, the protocol is
an open standard, clients like eco are independent software, and a
channel from 1995 can still be alive today with the same name. It is
chat as infrastructure rather than chat as a product, and once you
settle in, the simplicity is the feature.
