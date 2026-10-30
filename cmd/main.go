// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/omec-project/af/internal/logger"
	"github.com/omec-project/af/internal/service"
	"github.com/urfave/cli/v3"
)

var AF = &service.AF{}

func main() {
	app := &cli.Command{}
	app.Name = "af"
	logger.AppLog.Infoln(app.Name)
	app.Usage = "Application Function"
	app.UsageText = "af -cfg <af_config_file.yaml>"
	app.Action = action
	app.Flags = AF.GetCliCmd()
	if err := app.Run(context.Background(), os.Args); err != nil {
		logger.AppLog.Fatalf("AF run error: %v", err)
	}
}

func action(ctx context.Context, c *cli.Command) error {
	if err := AF.Initialize(ctx, c); err != nil {
		logger.CfgLog.Errorf("%+v", err)
		return fmt.Errorf("failed to initialize")
	}
	return AF.Start()
}
