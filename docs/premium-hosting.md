# Premium self-hosting

Premium is a one-time **$15 USD** unlock for the hosting panel on one launcher installation. The server runs on the buyer’s PC. Mojang supplies the Bedrock Dedicated Server download under its own terms. Broadcasting an existing server through the Friends tab stays free.

## Check your PC first

Open **Premium** in Mcbe2Friends. The panel shows the operating system, available logical CPUs, memory, and a suggested configuration before purchase. Native hosting targets Windows x64 and Ubuntu 22.04 LTS or newer on x64. Other x64 Linux distributions can work if their runtime libraries are compatible. macOS and ARM64 can run the free broadcaster. Mojang lists 4 GB of system RAM for the dedicated server.

| Suggested preset | Hardware | Player slots | View / tick distance |
| --- | --- | --- | --- |
| Small | Below the Balanced thresholds | 4 | 8 / 4 chunks |
| Balanced | At least 4 logical CPUs and about 8 GiB RAM | 8 | 12 / 4 chunks |
| Large | At least 8 logical CPUs and about 16 GiB RAM | 12 | 16 / 6 chunks |

These are starting settings. The launcher leaves CPU capacity for the operating system and respects Linux container limits when available. Reduce the view distance or player count if a heavily modified world runs slowly.

## Buy and start

1. Copy **Your PC code** and open **Get Premium on Discord**. Send the seller the code, arrange payment, and paste the key they return into **Activation key**.
2. Review the Minecraft terms and choose **Install & start server**. The launcher downloads the pinned Bedrock server directly from Minecraft, verifies its size and SHA-256, applies your preset, and checks the selected world through Bedrock’s local NetherNet signaling endpoint. Later launches reuse the installation and worlds.
3. Under **Players & Friends tab**, enter each player’s Xbox gamertag and click **Allow**. The new server starts with Microsoft online authentication and the allowlist enabled.
4. Players on your LAN can enter the local address and port in Bedrock. For the Friends tab, enter a reachable address under **Players & Friends tab**, choose **Friends setup**, and complete the normal Xbox broadcast setup.

For internet players, the included NetherNet server uses TCP signaling and UDP game traffic. Allow and forward the configured port for **both TCP and UDP** to the host PC, typically 19132. Configure those rules in your firewall and router. A public hostname or IP alone cannot provide inbound connectivity behind carrier-grade NAT. Keep the PC awake. **Stop server** saves the world and stops Bedrock. **Quit launcher** also stops broadcasting.

[Purchase on Discord](https://discord.gg/X5adbkPq) · [Official server download and requirements](https://www.minecraft.net/en-us/download/server/bedrock) · [Minecraft EULA](https://www.minecraft.net/eula)

## Manage the server

- **Server settings & worlds** edits standard server.properties. Change the port, slots, mode, difficulty, allowlist, cheats, view distance, tick distance, or threads. Select an existing world or enter a new name, save, and start. Unknown properties and comments are preserved. **Reload from server files** picks up manual edits.
- **Add-ons** imports behavior and resource packs from .mcpack, .mcaddon, or ZIP files up to 128 MiB. Enable each required pack for the selected world. Stop the server before editing. Disable a pack in every world before removing it. To replace an imported pack, remove the old version and import the new one. Manually installed packs and packs bundled inside the selected world also appear in the library.
- **World imports & backups** creates a .mcworld backup of a stopped world. Import a .mcworld or ZIP up to 512 MiB into a new world folder, then select it in settings. A backup includes the world and its pack references. Copy the separate behavior_packs and resource_packs folders when migrating add-ons.
- **Server console** displays recent output and accepts Bedrock commands such as `list`, `say Welcome!`, and `op "Xbox Gamertag"`. Use **Players & Friends tab** to add or remove allowlisted players without typing a command.
- **Open server folder** exposes normal world, allowlist, permissions, pack, and configuration files. The server is stored under `hosting/server` in the launcher's data folder. Backups live in `hosting/backups`. Stop Bedrock before making manual changes or copying its database.

Activation needs internet access once per launcher session. Preserve the private `hosting/installation.txt` and `hosting/premium-key.txt` files across reinstalls. For help with a purchase or a transfer, contact the project owner on Discord. [Premium support](https://discord.gg/X5adbkPq)
