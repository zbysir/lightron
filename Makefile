.PHONY: test vet menulib check-menulib examples

test:
	go vet ./...
	GOOS=windows go vet ./...
	GOOS=linux go vet ./...
	go test ./...

# The macOS menu library, universal; checked in (internal/macmenu/lightronmenu.dylib) so building an app needs no C
# compiler. Rebuild it after changing objc/menu.m.
menulib:
	clang -dynamiclib -fobjc-arc -framework Cocoa -O2 -arch arm64 -arch x86_64 -mmacosx-version-min=11.0 \
		-o internal/macmenu/lightronmenu.dylib internal/macmenu/objc/menu.m
	codesign --force --sign - internal/macmenu/lightronmenu.dylib

examples:
	go build -o /dev/null ./examples/...
