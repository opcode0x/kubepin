import AppKit
import SwiftUI

@main
struct KubePinApp: App {
    @StateObject private var controller = ForwardController()

    var body: some Scene {
        MenuBarExtra("KubePin", systemImage: "link") {
            MenuContent(controller: controller)
        }

        Settings {
            ConfigView(controller: controller)
        }
    }
}

private struct MenuContent: View {
    @ObservedObject var controller: ForwardController
    @Environment(\.openSettings) private var openSettings

    var body: some View {
        Text(controller.statusText)
        if let latest = controller.lines.last {
            Text(latest).lineLimit(1)
        }
        Divider()
        Button("Start Forwards") { controller.start() }
            .disabled(controller.isRunning)
        Button("Stop Forwards") { controller.stop() }
            .disabled(!controller.isRunning)
        Divider()
        Button("Settings…") { openSettings() }
        Button("Quit KubePin") {
            controller.stop()
            NSApp.terminate(nil)
        }
    }
}
