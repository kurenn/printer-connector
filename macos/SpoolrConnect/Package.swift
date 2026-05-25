// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "SpoolrConnect",
    platforms: [
        .macOS(.v13)
    ],
    targets: [
        .executableTarget(
            name: "SpoolrConnect",
            path: "Sources/SpoolrConnect"
        ),
        .testTarget(
            name: "SpoolrConnectTests",
            dependencies: ["SpoolrConnect"],
            path: "Tests/SpoolrConnectTests"
        )
    ]
)
