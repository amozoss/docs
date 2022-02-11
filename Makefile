build:
	hugo --config "config.toml,config.dcs.toml"
	hugo --config "config.toml,config.node.toml"

serve-dcs:
	hugo serve --config "config.toml,config.dcs.toml"

serve-node:
	hugo serve --config "config.toml,config.node.toml"