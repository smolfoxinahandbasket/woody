# Intro

Woody is a way to expose the IPC PINE connections for PCSX2 and RPCS3 as web APIs.

What does that let you do? Well, things like:
* use Twitch Rewards to change the character level in a game
* add an OBS overlay that shows the game currently being played
* create custom tools to read and update memory as the game is being played

# Supported Emulators

Woody has been tested against PCSX2 though it should work with RPCS3 as well. Adding more emulators that support PINE should be easy. 

# Installing

1. download the latest release from https://github.com/smolfoxinahandbasket/woody/releases
2. there is no step 2. You can just run it. Put it into a different directory if you're feeling fancy.

# Running

Woody does not require any configuration in most cases. You can run it and you should see log messages indicating when it is able to connect to a supported emulator. For Mac and Linux, you can set the TMPDIR (for Mac) or XDG_RUNTIME_DIR (for Linux) environment variables in case the socket file for the PINE connection is in a non-default location. In most cases, this shouldn't be needed.

With that said, you probably need to enable PINE connections for the emulator. For PCSX2, you first need to enable Advanced settings and then at the bottom of the `Advanced` settings section, you should find the checkbox to enable PINE. Restart PCSX2 and it should allow PINE connections.

If the default PINE port/slot has been changed in the emulator, it will need to be changed back to the default. Links to where to find the value for the default slot/port are in the References section below.

# Client Configuration

Connecting to Woody must be done on localhost at port 6669.

The PINE request type and parameters can be sent as either HTTP headers or URL parameters. For example, the following two usages of `curl` accomplish the same thing:
* `curl --header "Woody-Request-Type: Read64" --header "Woody-Address: 0x35459C" http://localhost:6669/`
* `curl http://localhost:6669/?Woody-Request-Type=Read64&Woody-Address=0x35459C`

Every parameter or header always starts with `Woody` (to prevent conflicts with any other headers that a client might automatically set).

For security reasons, Woody only allows for connections on localhost.

## Request Types

Woody supports all of the PINE request types defined in the standard (linked in the References section below) except for Events and Batch messages.

Listed below are the various requests and their parameters:

| Woody-Request-Type | HTTP Headers/Parameters |
|--------------------|------------|
| `Woody-Read8`, `Woody-Read16`, `Woody-Read32`, `Woody-Read64`, `Woody-Write8`, `Woody-Write16`, `Woody-Write32`, `Woody-Write64` | `Woody-Address` (for both reads and writes, prefixed by `0x` for hex based values), `Woody-Data` (for writes) |
| `Woody-State-State`, `Woody-Load-State` | `Woody-Slot` |
| `Woody-Version`, `Woody-Title`, `Woody-ID`, `Woody-UUID`, `Woody-Game-Version`, `Woody-Status` | none |

For response body includes the following JSON elements:

| Woody-Request-Type | JSON elements |
|--------------------|------------|
| `Woody-Read8`, `Woody-Read16`, `Woody-Read32`, `Woody-Read64` | `resultCode`, `memoryValue` |
| `Woody-Write8`, `Woody-Write16`, `Woody-Write32`, `Woody-Write64` | `resultCode` |
| `Woody-Version`| `resultCode`, `version` |
| `Woody-State-State`, `Woody-Load-State` | `resultCode` |
| `Woody-Title` | `resultCode`, `title` |
| `Woody-ID` | `resultCode`, `id` |
| `Woody-UUID` | `resultCode`, `uuid` |
| `Woody-Game-Version` | `resultCode`, `gameVersion` |
| `Woody-Status` | `resultCode`, `version` |

## HTTP Error Codes and PINE Response Codes

In general, if there's an error in Woody, a 400 HTTP response is sent
Otherwise the PINE result code is mapped:
* for a zero result code (successful PINE operation), a 200 HTTP response code is sent
* for a 255 result code (failed PINE operation), a 500 HTTP response code is sent
* for other result codes, a 501 HTTP response code is returned

# Tips

Testing requests with `curl --verbose` should let you see the headers getting sent and the JSON response. Browsers don't let you connect to `http://localhost:6669` because they aren't cool (they made some "security" choices).

When using Woody with streamer.bot, there's a few things to keep in mind:
* use the [Fetch URL sub-action](https://docs.streamer.bot/api/sub-actions/core/network/fetch-url) and parse the result as JSON. You can use `http://localhost:6669` as the URL. This can be used for every operation Woody supports (including both reading and writing to memory).
* you can do [math in sub-actions](https://docs.streamer.bot/guide/variables#inline-functions). This is useful for changing the value in memory based on the present value (e.g. to increase lives by one).
* use the [If/Else sub-action](https://docs.streamer.bot/api/sub-actions/core/logic/if-else) to check if the new value will be too low or too high. You might want to use [Update Redemption Status](https://docs.streamer.bot/api/sub-actions/twitch/rewards/update-redemption-status) to refund channel points if the new value is too high or too low.

To find memory addresses to modify, [gamehacking.org](https://gamehacking.org) is very helpful (for PCSX2 at least). Once you find the game that you're playing, download the codes in .pnach format (the patch format for PCSX2). Open the file in a text editor and you should see the addresses to modify but keep in mind that:
* the first digit in the address indicates whether it is for 1, 2, or 4 bytes, so keep that in mind when choosing the request type for Woody. Also, this first digit should be replaced with zero when passing it to Woody. See [this guide](https://forums.pcsx2.net/Thread-How-PNACH-files-work-2-0) on the PCXS2 forums for more details on the PNACH file format.
* the address is given in hex so when sending it to Woody, it needs to be prefixed with `0x`

# Building

Prereqs for building include:
* version 1.23.4 or newer of the [Go compiler](https://go.dev/) installed
* [Task](https://taskfile.dev/) installed

To build for the operating system and CPU architecture of the machine that you're on, run `task build`

To create binaries for all supported operating systems and CPU architectures, run `task build-all-binaries`

Note that for Windows binaries, you will need to add `.exe` to the end of the filename before Windows will let you run it.

# References

* implementation of the PINE protocol followed the draft standard: https://projects.govanify.com/govanify/pine/-/blob/3298a7dac42b2385a378720bf705fcd6a2eb553f/standard/draft.dtd
* the default ports/slots for each emulator:
    * PCSX2 uses 28011, which is based on https://github.com/PCSX2/pcsx2/blob/4dafea65f256f2fa342f5bd33c624bbc14e6e0f0/pcsx2/PINE.h#L13
    * RPCS3 uses 28012, which is based on https://github.com/RPCS3/rpcs3/blob/92d07072915b99917892dd7833c06eb44a09e234/rpcs3/Emu/IPC_config.h#L8