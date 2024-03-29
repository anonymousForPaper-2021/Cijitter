load("//tools:defs.bzl", "go_binary", "pkg_deb", "pkg_tar")

package(licenses = ["notice"])

go_binary(
    name = "runsc",
    srcs = [
        "main.go",
        "version.go",
    ],
    pure = True,
    visibility = [
        "//visibility:public",
    ],
    x_defs = {"main.version": "{STABLE_VERSION}"},
    deps = [
        "//pkg/log",
        "//pkg/refs",
        "//pkg/sentry/platform",
        "//runsc/boot",
        "//runsc/cmd",
        "//runsc/flag",
        "//runsc/specutils",
        "@com_github_google_subcommands//:go_default_library",
        "//pkg/maid",
    ],
)

# The runsc-race target is a race-compatible BUILD target. This must be built
# via: bazel build --features=race :runsc-race
#
# This is neccessary because the race feature must apply to all dependencies
# due a bug in gazelle file selection.  The pure attribute must be off because
# the race detector requires linking with non-Go components, although we still
# require a static binary.
#
# Note that in the future this might be convertible to a compatible target by
# using the pure and static attributes within a select function, but select is
# not currently compatible with string attributes [1].
#
# [1] https://github.com/bazelbuild/bazel/issues/1698
go_binary(
    name = "runsc-race",
    srcs = [
        "main.go",
        "version.go",
    ],
    static = True,
    visibility = [
        "//visibility:public",
    ],
    x_defs = {"main.version": "{STABLE_VERSION}"},
    deps = [
        "//pkg/log",
        "//pkg/refs",
        "//pkg/sentry/platform",
        "//runsc/boot",
        "//runsc/cmd",
        "//runsc/flag",
        "//runsc/specutils",
        "@com_github_google_subcommands//:go_default_library",
    ],
)

pkg_tar(
    name = "debian-bin",
    srcs = [
        ":runsc",
        "//shim/v1:gvisor-containerd-shim",
        "//shim/v2:containerd-shim-runsc-v1",
    ],
    mode = "0755",
    package_dir = "/usr/bin",
)

pkg_tar(
    name = "debian-data",
    extension = "tar.gz",
    deps = [
        ":debian-bin",
        "//shim:config",
    ],
)

genrule(
    name = "deb-version",
    # Note that runsc must appear in the srcs parameter and not the tools
    # parameter, otherwise it will not be stamped. This is reasonable, as tools
    # may be encoded differently in the build graph (cached more aggressively
    # because they are assumes to be hermetic).
    srcs = [":runsc"],
    outs = ["version.txt"],
    # Note that the little dance here is necessary because files in the $(SRCS)
    # attribute are not executable by default, and we can't touch in place.
    cmd = "cp $(location :runsc) $(@D)/runsc && \
        chmod a+x $(@D)/runsc && \
        $(@D)/runsc -version | grep version | sed 's/^[^0-9]*//' > $@ && \
        rm -f $(@D)/runsc",
    stamp = 1,
)

pkg_deb(
    name = "runsc-debian",
    architecture = "amd64",
    data = ":debian-data",
    # Note that the description_file will be flatten (all newlines removed),
    # and therefore it is kept to a simple one-line description. The expected
    # format for debian packages is "short summary\nLonger explanation of
    # tool." and this is impossible with the flattening.
    description_file = "debian/description",
    homepage = "https://gvisor.dev/",
    maintainer = "The gVisor Authors <gvisor-dev@googlegroups.com>",
    package = "runsc",
    postinst = "debian/postinst.sh",
    version_file = ":version.txt",
    visibility = [
        "//visibility:public",
    ],
)

sh_test(
    name = "version_test",
    size = "small",
    srcs = ["version_test.sh"],
    args = ["$(location :runsc)"],
    data = [":runsc"],
    tags = ["noguitar"],
)
