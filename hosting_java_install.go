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

type javaComponent struct {
	asset
	url string
}

var (
	mojangJar    = javaComponent{asset{"Minecraft Java " + javaVersion, "d052f14d7a173734fba553711e5b570162e2f2a313267ee31a21b975a679be64", 62294556}, "https://piston-data.mojang.com/v1/objects/33680f5f2ac32864d6d7cf5e56a705fdb3e05f4c/server.jar"}
	paperJar     = javaComponent{asset{"Paper 26.3 build 257", "64a5bd44910cb982b2648e9a35036cc95a6de4d561e15812aecf5411f84f7b01", 55305512}, "https://fill-data.papermc.io/v1/objects/64a5bd44910cb982b2648e9a35036cc95a6de4d561e15812aecf5411f84f7b01/paper-26.3-257.jar"}
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
	java, err := ensureRuntime(ctx, cache, serverRuntime, report)
	if err != nil {
		return "", err
	}
	jar := filepath.Join(dir, "server.jar")
	if c.Engine == "vanilla" {
		return java, mojangJar.install(ctx, cache, jar, report)
	}
	if err = paperJar.install(ctx, cache, jar, report); err != nil {
		return "", err
	}
	if err = mojangJar.install(ctx, cache, filepath.Join(dir, "cache", "mojang_"+javaVersion+".jar"), report); err != nil {
		return "", err
	}
	plugins := filepath.Join(dir, "plugins")
	if err = os.MkdirAll(plugins, 0700); err != nil {
		return "", err
	}
	for _, plugin := range []struct {
		file   string
		source javaComponent
	}{{"Geyser-Spigot.jar", geyserJar}, {"floodgate-spigot.jar", floodgateJar}} {
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
