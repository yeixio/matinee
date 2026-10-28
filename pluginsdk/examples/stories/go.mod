module github.com/yeixio/matinee/pluginsdk/examples/stories

go 1.24

require github.com/yeixio/matinee/pluginsdk v0.0.0

// The example builds against the SDK next to it. A plugin of your own
// requires a released SDK instead: go get github.com/yeixio/matinee/pluginsdk@latest
replace github.com/yeixio/matinee/pluginsdk => ../..
