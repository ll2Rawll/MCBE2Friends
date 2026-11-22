# Mcbe2Friends

Mcbe2Friends puts an existing Minecraft Bedrock or Geyser server in the in-game Friends tab using MCXboxBroadcast Standalone. Run the launcher on a computer that can reach the server and stay on while people play.

## Open the app

Extract the download for your computer. When upgrading from 1.3, stop any running services in the old panel and choose **Close launcher** before opening the new download. The new launcher reads your existing key, worlds, and broadcast settings.

- **Windows 10 or 11, 64-bit** — open **Mcbe2Friends.exe**.
- **macOS, Apple silicon or Intel** — open **Mcbe2Friends.app**. You can move it to Applications.
- **Linux, x64 or ARM64** — open **Mcbe2Friends** in the extracted folder, or run `./Mcbe2Friends` there.

The app opens its controls in your default browser. These portable builds have no publisher signature or Apple notarization, so your operating system may ask you to approve the app.

From version 1.2, GitHub signs the official release archives. To verify your download with the GitHub CLI, save the release’s signature file in the same folder and run the command below. Substitute your archive’s filename on macOS or Linux. [Download the signature file for 1.4.0](https://github.com/ll2Rawll/MCBE2Friends/releases/download/v1.4.0/Mcbe2Friends-attestation.json)

```sh
gh attestation verify Mcbe2Friends-Windows-x64.zip --bundle Mcbe2Friends-attestation.json --repo ll2Rawll/MCBE2Friends --signer-workflow ll2Rawll/MCBE2Friends/.github/workflows/release-downloads.yml --deny-self-hosted-runners
```

## Host a server on this PC

Open **Premium** to check hardware support, activate a key, and choose **Bedrock Edition** or **Java Edition**. Create Vanilla Java, Paper, or Paper with Geyser and Floodgate. The panel manages regular world folders, Bedrock add-ons, and Paper plugins. [Premium setup and management guide](https://github.com/ll2Rawll/MCBE2Friends/blob/main/docs/premium-hosting.md)

## Start your first broadcast

1. Enter the server’s join address and Bedrock port. The default port is **19132**. You can paste an address with its port, such as `play.example.net:19132`. Choose **Continue**.
2. Use a dedicated Microsoft account with an Xbox gamertag. Choose whether to accept incoming friend requests automatically, then select **Connect & start**.
3. On first use the launcher checks for Java 21 and downloads the components it needs. Open the Microsoft sign-in page, enter the code shown in the launcher, and complete sign-in there.
4. Wait for **Broadcast live**. Have friends add the displayed gamertag on Xbox. Once their requests are accepted, they can open **Minecraft Bedrock → Play → Friends** and select the session.

A live status confirms that Xbox created the session. Joining from another Bedrock client also checks that the server can receive players.

## Closing and restarting

Once you turn on the broadcast or your Premium server, choose **Close launcher** or close the browser tab. Keep the PC awake and connected while friends play. Open Mcbe2Friends again to manage the same session.

After a reboot or power interruption, Mcbe2Friends resumes each service you left on when you next sign in to the same computer account. The controls stay closed. The worker verifies your Premium key and opens each Bedrock or Java server you left on. If Microsoft needs a fresh sign-in, reopen the launcher for the device code.

**Stop broadcast** and **Stop server** turn off that service and its automatic restart. The server console’s `stop` command does the same for Bedrock. To cancel a pending restart while offline, use **Cancel broadcast restart** or **Cancel server restart**. To switch Xbox accounts, stop the broadcast and choose **Disconnect account**.

When upgrading from 1.2, choose **Quit launcher** in the old version once, open 1.3, and start the services you want to keep on.

## Unattended startup

To bring a host back **before anyone signs in**, start the service in Mcbe2Friends once, then use the one-time option for that computer. The worker runs under your own operating-system account.

- **Windows 10 or 11** — open **Task Scheduler** as an administrator and choose **Import Task**. Open `%LOCALAPPDATA%\Mcbe2Friends\background\start-before-sign-in.xml`, name the task **Mcbe2Friends**, and save it. It runs at system startup with your account’s normal permissions. You can remove it from Task Scheduler.
- **Linux with systemd** — run `loginctl enable-linger "$USER"` from your hosting account. Allow the system permission prompt if shown. This allows that account’s user services to run at boot and after logout. Mcbe2Friends has already installed its user service. To reverse the account-level change, use `loginctl disable-linger "$USER"`.
- **macOS** — for unattended hosting or broadcasting, run the command below in Terminal after starting a service once. It installs the generated launch daemon for your account. Remove `/Library/LaunchDaemons/app.mcbe2friends.boot.plist` to disable it at the next boot. A Mac with FileVault requires its startup disk to be unlocked before macOS can run the daemon.

```sh
sudo install -o root -g wheel -m 644 "$HOME/Library/Application Support/Mcbe2Friends/background/start-before-sign-in.plist" /Library/LaunchDaemons/app.mcbe2friends.boot.plist
```

These paths assume the default data folder. If you use `-data-dir`, look in that folder’s `background` directory. Other Linux service managers can invoke `Mcbe2Friends -resume -data-dir "/path/to/profile"` at boot as the hosting user. When all services are off, a startup invocation exits immediately.

## If setup gets stuck

- **Server did not answer its status query** — use the Bedrock listener’s address and port. This build expects a server compatible with Bedrock 1.26.60 and NetherNet HTTP signaling. Check the server’s version and network configuration if friends can see the entry but cannot join.
- **No joinable session** — check that the launcher shows **Broadcast live**, the friend request has been accepted, and both Xbox profiles allow online multiplayer. Check **Activity** for connection problems.
- **Sign-in timed out** — cancel or retry setup to request a fresh Microsoft code.
- **Interrupted download** — check the internet connection, then try again. Completed components are reused. The initial download is about 20 MB with Java 21 installed or about 70 MB when a private Java runtime is needed.
- **Browser did not open** — run the launcher from a terminal with `-no-browser` and open the local URL it prints. You can close the terminal afterward. Keep that URL on this computer.

## Local data and components

User settings, MCXboxBroadcast’s account cache, and downloaded components are stored under the current user’s profile.

| Platform | Default folder |
| --- | --- |
| Windows | `%LOCALAPPDATA%\Mcbe2Friends` |
| macOS | `~/Library/Application Support/Mcbe2Friends` |
| Linux | `$XDG_CONFIG_HOME/Mcbe2Friends` or `~/.config/Mcbe2Friends` |

The launcher listens on the loopback interface and requires a per-launch key for controls. Device codes are shown only during sign-in. The Activity view keeps recognized events in memory for the current launch. The broadcaster’s cache contains private sign-in data.

This version downloads checksum-verified MCXboxBroadcast build 165 and, when needed, Eclipse Temurin Java 21.0.13.1+2 from their official releases. The app writes its own upstream configuration, preserves the account’s existing friends, disables automatic friend expiry, and leaves initial game invites off.

- [MCXboxBroadcast build 165](https://github.com/MCXboxBroadcast/Broadcaster/releases/tag/165)
- [Eclipse Temurin JRE 21 release](https://github.com/adoptium/temurin21-binaries/releases/tag/jdk-21.0.13.1%2B2)
- [MCXboxBroadcast project and account guidance](https://github.com/MCXboxBroadcast/Broadcaster)

## Build from source

Use Go 1.27.4 or later. The launcher verifies Premium approvals with Sigstore's Go library. From the project directory, run

```sh
go test ./...
go build -trimpath -o Mcbe2Friends .
./Mcbe2Friends
```

On Windows, use `go build -trimpath -ldflags="-H windowsgui" -o Mcbe2Friends.exe .`. To build all portable downloads, run `python3 packaging/build.py` with Go on PATH. The script places the packages in `dist/`. The Windows icon and application manifest are included in `resource_windows_amd64.syso`. They can be regenerated with `rsrc -arch amd64 -ico packaging/Mcbe2Friends.ico -manifest packaging/launcher.manifest -o resource_windows_amd64.syso`.

The included Go and font license notices are in the licenses folder of each download and under `packaging/licenses` in the source.
