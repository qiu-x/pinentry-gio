# Local-checkout build
pkgname=pinentry-gio
pkgver=0.1.0
pkgrel=1
pkgdesc='GPG pinentry dialog written in Go'
arch=('x86_64' 'aarch64')
license=('BSD-2-Clause')
depends=('glibc' 'libglvnd' 'wayland' 'libxkbcommon' 'libxkbcommon-x11'
         'libx11' 'libxcursor' 'libxfixes' 'libxcb')
makedepends=('go>=1.24' 'pkgconf')
checkdepends=('dbus')
optdepends=('xdg-desktop-portal: desktop dark-theme detection')
options=('!lto' '!debug')

prepare() {
  mkdir -p "$srcdir/$pkgname"
  cp "$startdir/"{go.mod,go.sum,*.go,LICENSE} "$srcdir/$pkgname/"
  cp -r "$startdir/theme" "$srcdir/$pkgname/"
  cd "$srcdir/$pkgname"
  export GOPATH="$srcdir/go"
  go mod download -modcacherw
}

build() {
  cd "$srcdir/$pkgname"
  export GOPATH="$srcdir/go"
  export CGO_ENABLED=1
  export CGO_CPPFLAGS="$CPPFLAGS"
  export CGO_CFLAGS="$CFLAGS"
  export CGO_CXXFLAGS="$CXXFLAGS"
  export CGO_LDFLAGS="$LDFLAGS"
  go build -mod=readonly -modcacherw -trimpath -buildmode=pie \
    -tags novulkan -ldflags='-linkmode=external' -o "$pkgname" .
}

package() {
  cd "$srcdir/$pkgname"
  install -Dm755 "$pkgname" "$pkgdir/usr/bin/$pkgname"
  install -Dm644 LICENSE "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
