package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"go.airlance.org/server/internal/bootstrap"
	"go.airlance.org/server/internal/infrastructure/config"
)

func newWallpaperCommand() *cobra.Command {
	var (
		name string
		sort int32
	)

	withConfig := func(work func(*cobra.Command, config.Config, []string) error) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(os.Getenv)
			if err != nil {
				return err
			}

			return work(cmd, cfg, args)
		}
	}

	root := &cobra.Command{
		Use:   "wallpaper",
		Short: "Manage the default chat wallpapers every account can pick",
		Args:  cobra.NoArgs,
		RunE:  needsSubcommand,
	}

	add := &cobra.Command{
		Use:   "add FILE",
		Short: "Upload an image and add it to the catalog",
		Args:  cobra.ExactArgs(1),
		RunE: withConfig(func(cmd *cobra.Command, cfg config.Config, args []string) error {
			id, err := bootstrap.WallpaperAdd(cmd.Context(), cfg, args[0], name, sort)
			if err != nil {
				return err
			}

			fmt.Println(id)

			return nil
		}),
	}
	add.Flags().StringVar(&name, "name", "", "the name people see")
	add.Flags().Int32Var(&sort, "sort", 0, "the place in the list, smaller goes first")

	if err := add.MarkFlagRequired("name"); err != nil {
		panic(err)
	}

	root.AddCommand(
		add,
		&cobra.Command{
			Use:   "list",
			Short: "Show the catalog",
			Args:  cobra.NoArgs,
			RunE: withConfig(func(cmd *cobra.Command, cfg config.Config, _ []string) error {
				list, err := bootstrap.WallpaperList(cmd.Context(), cfg)
				if err != nil {
					return err
				}

				for _, next := range list {
					fmt.Printf("%s\t%d\t%s\n", next.ID, next.Sort, next.Name)
				}

				return nil
			}),
		},
		&cobra.Command{
			Use:   "remove ID",
			Short: "Remove a wallpaper from the catalog and the storage",
			Args:  cobra.ExactArgs(1),
			RunE: withConfig(func(cmd *cobra.Command, cfg config.Config, args []string) error {
				return bootstrap.WallpaperRemove(cmd.Context(), cfg, args[0])
			}),
		},
	)

	return root
}
