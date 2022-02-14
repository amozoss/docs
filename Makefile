build:
	hugo --config "config.toml,config.dcs.toml"
	hugo --config "config.toml,config.node.toml"
	cp content-extra/index.html public/index.html

convert:
	go run .

convert-ci:
	go run . --skip-worktree --skip-download

serve-dcs:
	hugo serve --config "config.toml,config.dcs.toml"

serve-node:
	hugo serve --config "config.toml,config.node.toml"x