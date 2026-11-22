package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"go.yaml.in/yaml/v3"
)

var serverRuntime = javaRuntime{"25", "25.0.5.1+2", "https://github.com/adoptium/temurin25-binaries/releases/download/jdk-25.0.5.1%2B2/", map[string]asset{
	"windows/amd64": {"OpenJDK25U-jre_x64_windows_hotspot_25.0.5.1_2.zip", "3260a1b9899f30ac80d737daafc053042c525249f52d066101fbf81d78d73f25", 58599439},
	"linux/amd64":   {"OpenJDK25U-jre_x64_linux_hotspot_25.0.5.1_2.tar.gz", "2edef9e4f81ec5ee426d023336d4cfbd7b4644e99884e7a73a6a867b70b17acf", 61751939},
	"linux/arm64":   {"OpenJDK25U-jre_aarch64_linux_hotspot_25.0.5.1_2.tar.gz", "c6161dcfb365024fbfbcda265343d562d6f821c78dd8e7aeaeaf294893f6ff8b", 60526698},
	"darwin/amd64":  {"OpenJDK25U-jre_x64_mac_hotspot_25.0.5.1_2.tar.gz", "fac7e81bba96b45cfc3381dbc8bbca6ffd129e3e30aed70493245b61dde7d639", 42215950},
	"darwin/arm64":  {"OpenJDK25U-jre_aarch64_mac_hotspot_25.0.5.1_2.tar.gz", "70adbb3ce5cf924c8b60dd84272fdd5fba6d021014aa0c1f5cf66a198bd270c5", 56303568},
}}

var runtime17 = javaRuntime{"17", "17.0.21.1+2", "https://github.com/adoptium/temurin17-binaries/releases/download/jdk-17.0.21.1%2B2/", map[string]asset{
	"windows/amd64": {"OpenJDK17U-jre_x64_windows_hotspot_17.0.21.1_2.zip", "739d8f2fd286dc2736d55cc8c9260980baefd275ad48cda242335bb0f08e0795", 43874958},
	"linux/amd64":   {"OpenJDK17U-jre_x64_linux_hotspot_17.0.21.1_2.tar.gz", "5a092a0bc21de4c3d855480edd7945be9cfe157b38cdb84dac280511893bb009", 46737725},
	"linux/arm64":   {"OpenJDK17U-jre_aarch64_linux_hotspot_17.0.21.1_2.tar.gz", "ed968eb2cde06ba38c3d957956cab7faced9181aaeebed4904f89e2e6efa8d11", 46121315},
	"darwin/amd64":  {"OpenJDK17U-jre_x64_mac_hotspot_17.0.21.1_2.tar.gz", "5b92ba9954ca63470e896cb50acd17c94dc95d03208803617d04dc53ea2c845b", 37728126},
	"darwin/arm64":  {"OpenJDK17U-jre_aarch64_mac_hotspot_17.0.21.1_2.tar.gz", "398046289ca1acef2d3800d74c55a31f08b2fee32a17ee7c1c4e4ad7489f8311", 42828495},
}}

type javaComponent struct {
	asset
	url string
}

var (
	viaJar       = javaComponent{asset{"ViaVersion 5.12.2", "c6af11f50e14bc1ba239f5a90be653cce85feee30c473a4b53336b4d5527d790369cf43843f7cb06d3ac3a34fc207e7203436737ca719070644101472453401b", 6508809}, "https://cdn.modrinth.com/data/P1OZGk5p/versions/Fkfcr89K/ViaVersion-5.12.2.jar"}
	geyserJar    = javaComponent{asset{"Geyser 2.12.1 build 1306", "cf8adf34e71a5086fc53019b9b5086136ce22c38e74b7a93a9f24e90a02d6e6b", 52219160}, "https://download.geysermc.org/v2/projects/geyser/versions/2.12.1/builds/1306/downloads/spigot"}
	floodgateJar = javaComponent{asset{"Floodgate 2.2.5 build 141", "21570aff9ce17d6983928e8552777760e1ede5050026b04c686b0ae112e6fd7e", 11561096}, "https://download.geysermc.org/v2/projects/floodgate/versions/2.2.5/builds/141/downloads/spigot"}
)

func (c javaComponent) install(ctx context.Context, cache, dst string, report reportProgress) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	report("Checking "+c.name, -1)
	if verified(dst, c.digest) {
		return nil
	}
	cached := filepath.Join(cache, c.digest+".jar")
	if !verified(cached, c.digest) {
		if err := download(ctx, c.url, cached, c.asset, func(n int) { report("Downloading "+c.name, n) }); err != nil {
			return fmt.Errorf("%s: %w", c.name, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	in, err := os.Open(cached)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), ".jar-")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(out.Name(), dst)
}

// Preserve custom Geyser settings and comments outside the fields owned by the launcher.
func configureGeyser(dir string, port int) error {
	dir = filepath.Join(dir, "plugins", "Geyser-Spigot")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.yml")
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	doc := yaml.Node{}
	if len(b) == 0 {
		b = []byte("config-version: 12\nbedrock: {}\njava: {}\n")
	}
	if err = yaml.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("Read Geyser config.yml: %w", err)
	}
	if len(doc.Content) != 1 {
		return errors.New("Geyser config.yml must contain a settings map.")
	}
	set := func(value, tag string, keys ...string) error {
		node := doc.Content[0]
		for _, k := range keys {
			if node.Kind != yaml.MappingNode {
				return fmt.Errorf("Check the %s settings in Geyser config.yml.", k)
			}
			var child *yaml.Node
			for i := 0; i < len(node.Content); i += 2 {
				if node.Content[i].Value == k {
					child = node.Content[i+1]
					break
				}
			}
			if child == nil {
				child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}, child)
			}
			node = child
		}
		node.Kind, node.Tag, node.Value, node.Content = yaml.ScalarNode, tag, value, nil
		return nil
	}
	for _, field := range []struct {
		value, tag string
		keys       []string
	}{
		{strconv.Itoa(port), "!!int", []string{"bedrock", "port"}}, {"0", "!!int", []string{"bedrock", "webrtc-port"}},
		{"0.0.0.0", "!!str", []string{"bedrock", "address"}}, {"false", "!!bool", []string{"bedrock", "clone-remote-port"}},
		{"nethernet", "!!str", []string{"bedrock", "transport"}}, {"builtin", "!!str", []string{"bedrock", "signaling", "mode"}}, {"floodgate", "!!str", []string{"java", "auth-type"}},
	} {
		if err = set(field.value, field.tag, field.keys...); err != nil {
			return err
		}
	}
	b, err = yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	return atomicWrite(path, b)
}
func (h *Hosting) installJava(ctx context.Context, dir string, c JavaConfig, report reportProgress) (string, error) {
	select {
	case h.javaInstall <- struct{}{}:
		defer func() { <-h.javaInstall }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	cache := filepath.Join(h.root, "java", "components")
	if err := os.MkdirAll(cache, 0700); err != nil {
		return "", err
	}
	release, err := findJavaRelease(c.Version)
	if err != nil {
		return "", err
	}
	java, err := ensureRuntime(ctx, cache, release.runtime(), report)
	if err != nil {
		return "", err
	}
	jar := filepath.Join(dir, "server.jar")
	mojangJar := release.Vanilla.component()
	if c.Engine == "vanilla" || c.Engine == "fabric" {
		if err = mojangJar.install(ctx, cache, jar, report); err != nil {
			return "", err
		}
		if c.Engine == "fabric" {
			if err = os.MkdirAll(filepath.Join(dir, "mods"), 0700); err != nil {
				return "", err
			}
			for _, coordinate := range release.Fabric {
				lib := catalog.Libraries[coordinate]
				if err = lib.component().install(ctx, cache, fabricLibraryPath(dir, lib), report); err != nil {
					return "", err
				}
			}
		}
		return java, nil
	}
	if release.Paper == nil {
		return "", errors.New("Paper is unavailable for this Minecraft version.")
	}
	paperJar := release.Paper.component()
	if err = paperJar.install(ctx, cache, jar, report); err != nil {
		return "", err
	}
	if err = mojangJar.install(ctx, cache, filepath.Join(dir, "cache", "mojang_"+c.Version+".jar"), report); err != nil {
		return "", err
	}
	plugins := filepath.Join(dir, "plugins")
	if err = os.MkdirAll(plugins, 0700); err != nil {
		return "", err
	}
	type crossplayPlugin struct {
		file   string
		source javaComponent
	}
	managed := []crossplayPlugin{{"Geyser-Spigot.jar", geyserJar}, {"floodgate-spigot.jar", floodgateJar}}
	if c.Version != geyserGameVersion && release.supportsGeyser() {
		if c.Geyser {
			if err = checkViaPlugin(plugins); err != nil {
				return "", err
			}
		}
		managed = append(managed, crossplayPlugin{viaFile, viaJar})
	}
	for _, plugin := range managed {
		path := filepath.Join(plugins, plugin.file)
		if c.Geyser {
			if err = plugin.source.install(ctx, cache, path, report); err != nil {
				return "", err
			}
		} else if _, err = os.Lstat(path); err == nil {
			if !verified(path, plugin.source.digest) {
				return "", fmt.Errorf("Move the custom %s out of plugins before starting Java-only mode.", plugin.file)
			}
			if err = os.Remove(path); err != nil {
				return "", err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if c.Geyser {
		err = configureGeyser(dir, c.BedrockPort)
	} else {
		err = nil
	}
	return java, err
}

func managedJavaRuntime(h *Hosting, path string) bool {
	cache := filepath.Join(h.root, "java", "components")
	rel, err := filepath.Rel(cache, path)
	return err == nil && filepath.IsLocal(rel)
}
