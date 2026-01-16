# Copyright 2021-2026 Gentoo Authors
# Distributed under the terms of the GNU General Public License v2

EAPI=8

inherit go-module shell-completion

DESCRIPTION="Real-time logging dashboard for Kubernetes"

HOMEPAGE="https://github.com/kubetail-org/kubetail"

SRC_URI="https://github.com/kubetail-org/kubetail/releases/download/cli%2Fv${PV}/kubetail-${PV}-vendored.tar.gz"

S="${WORKDIR}/kubetail-${PV}/modules/cli"

LICENSE="Apache-2.0"

SLOT="0"

KEYWORDS="~amd64 ~arm64"

BDEPEND=">=dev-lang/go-1.24.7"

src_compile() {
	(
		GOWORK=off \
		CGO_ENABLED=0 \
		ego build \
			-mod=vendor \
			-ldflags "-X github.com/kubetail-org/kubetail/modules/cli/cmd.version=${PV}" \
			-o "${PN}" \
			.
	)

	"./${PN}" completion bash > "${PN}.bash" || die
	"./${PN}" completion zsh > "${PN}.zsh" || die
	"./${PN}" completion fish > "${PN}.fish" || die
}

src_install() {
	dobin "${PN}"

	newbashcomp "${PN}.bash" "${PN}"
	newzshcomp "${PN}.zsh" "_${PN}"
	dofishcomp "${PN}.fish"
}
