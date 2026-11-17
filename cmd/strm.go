package cmd

import (
	"fmt"
	"github.com/OpenListTeam/OpenList/v4/cmd/flags"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/product"
	"github.com/spf13/cobra"
	"net/http"
	"time"
)

func init() {
	RootCmd.AddCommand(&cobra.Command{
		Use: "strm-server", Short: "启动开源网盘STRM中间件",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.Dev {
				return fmt.Errorf("产品命令不使用上游--dev内存库和默认密码；请用独立--data目录开发")
			}
			// long: 专用命令强制产品策略，默认独立数据目录，旧config不能取消安全门槛。
			conf.STRMCommandMode = true
			product.RestrictNewFiles()
			if !cmd.Flags().Changed("data") && !RootCmd.PersistentFlags().Changed("data") {
				flags.DataDir = "data-strm"
			}
			ServerCmd.Run(cmd, args)
			return nil
		},
	})
	check := &cobra.Command{Use: "healthcheck", Short: "检查中间件就绪状态", RunE: func(cmd *cobra.Command, _ []string) error {
		address, _ := cmd.Flags().GetString("url")
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(address)
		if err != nil {
			return fmt.Errorf("中间件尚未就绪")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("中间件尚未就绪（HTTP %d）", resp.StatusCode)
		}
		return nil
	}}
	check.Flags().String("url", "http://127.0.0.1:5344/ready", "就绪检查地址")
	RootCmd.AddCommand(check)
}
