export default {
  "*.go": ["gofmt -w"],
  "*.{js,mjs,json,md,yml,yaml}": ["prettier --write"],
};
