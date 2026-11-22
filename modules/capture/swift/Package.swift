// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Capture",
    platforms: [.macOS(.v13)],
    products: [.library(name: "Capture", type: .dynamic, targets: ["Capture"])],
    dependencies: [],
    targets: [
        .target(name: "Capture"),
    ],
    swiftLanguageModes: [.v5]
)
