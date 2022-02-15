build:
	hugo

convert:
	go run .

convert-ci:
	go run . --skip-worktree --skip-download

serve:
	hugo serve