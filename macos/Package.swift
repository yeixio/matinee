// swift-tools-version:5.9
// Matinee Server for macOS: a menu-bar app that runs the Matinee server.
// scripts/build-mac-app.sh builds it into Matinee Server.app with the server
// and FFmpeg inside; `swift test` runs the tests.
import PackageDescription

let package = Package(
    name: "MatineeServer",
    platforms: [.macOS(.v13)],
    dependencies: [
        .package(url: "https://github.com/sparkle-project/Sparkle", exact: "2.6.4"),
    ],
    targets: [
        .executableTarget(
            name: "MatineeServer",
            dependencies: ["MatineeServerCore", .product(name: "Sparkle", package: "Sparkle")],
            path: "Sources/MatineeServer",
            // Sparkle.framework sits in Contents/Frameworks.
            linkerSettings: [.unsafeFlags(["-Xlinker", "-rpath", "-Xlinker", "@executable_path/../Frameworks"])]
        ),
        .target(name: "MatineeServerCore", path: "Sources/MatineeServerCore"),
        .testTarget(name: "MatineeServerCoreTests", dependencies: ["MatineeServerCore"], path: "Tests/MatineeServerCoreTests"),
    ]
)
