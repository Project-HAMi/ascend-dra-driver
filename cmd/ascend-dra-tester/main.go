/*
 * Copyright 2025 The HAMi Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
	"sigs.k8s.io/yaml"
)

const (
	defaultOutputFormat = "json"
)

type Flags struct {
	nodeName   string
	output     string
	prettyJSON bool
}

func main() {
	if err := newApp().Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newApp() *cli.App {
	flags := &Flags{}
	return &cli.App{
		Name:        "ascend-dra-tester",
		Usage:       "Standalone NPU discovery tester for the Ascend DRA driver.",
		Description: "ascend-dra-tester re-implements the NPU device discovery path of ascend-dra-kubeletplugin and emits the discovered devices as a Kubernetes ResourceSlice for manual verification.",
		HideHelpCommand: true,
		ArgsUsage:       " ",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "node-name",
				Usage:       "Name of the node that will own the generated ResourceSlice.",
				Required:    true,
				Destination: &flags.nodeName,
				EnvVars:     []string{"NODE_NAME"},
			},
			&cli.StringFlag{
				Name:        "output",
				Aliases:     []string{"o"},
				Usage:       "Output format. One of: json, yaml.",
				Value:       defaultOutputFormat,
				Destination: &flags.output,
			},
			&cli.BoolFlag{
				Name:        "pretty",
				Usage:       "Indent JSON output for human readability. Ignored for yaml.",
				Value:       true,
				Destination: &flags.prettyJSON,
			},
		},
		Action: func(c *cli.Context) error {
			if c.Args().Len() > 0 {
				return fmt.Errorf("unexpected arguments: %v", c.Args().Slice())
			}
			if flags.output != "json" && flags.output != "yaml" {
				return fmt.Errorf("invalid output format %q, expected json or yaml", flags.output)
			}
			return run(flags)
		},
	}
}

func run(flags *Flags) error {
	result, err := DiscoverNPUDevices(flags.nodeName)
	if err != nil {
		return err
	}

	switch flags.output {
	case "yaml":
		out, err := yaml.Marshal(result.Slice)
		if err != nil {
			return fmt.Errorf("failed to marshal ResourceSlice to yaml: %w", err)
		}
		fmt.Println(string(out))
	case "json":
		var out []byte
		var err error
		if flags.prettyJSON {
			out, err = json.MarshalIndent(result.Slice, "", "  ")
		} else {
			out, err = json.Marshal(result.Slice)
		}
		if err != nil {
			return fmt.Errorf("failed to marshal ResourceSlice to json: %w", err)
		}
		fmt.Println(string(out))
	default:
		return fmt.Errorf("unsupported output format: %s", flags.output)
	}
	return nil
}
