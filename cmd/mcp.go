package cmd

import (
	"github.com/spf13/cobra"

	"github.com/bubblmail/bubblmail/mcpserver"
)

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.Flags().Bool("read-only", false, "Expose only read tools; disable mutating tools (move/mark read/star/tag)")
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run a Model Context Protocol server over stdio",
	Long: `Run an MCP server that exposes the mailbox to LLM agents.

The server communicates over stdio using JSON-RPC, so it is meant to be
launched by an MCP client (e.g. Claude Desktop or Claude Code) rather than
run interactively. Read tools are served from the local cache; write tools
(move, mark read, star, tag) connect to IMAP on demand. Pass --read-only to
expose the read tools only.

Example client config entry:

  {
    "mcpServers": {
      "bubblmail": { "command": "bubblmail", "args": ["mcp"] }
    }
  }`,
	RunE: func(cmd *cobra.Command, args []string) error {
		readOnly, _ := cmd.Flags().GetBool("read-only")
		cfg, store, err := loadConfigAndStore()
		if err != nil {
			return err
		}
		defer store.Close()
		return mcpserver.Serve(cfg, store, readOnly)
	},
}
