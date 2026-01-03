import React
import UIKit

@main
class AppDelegate: UIResponder, UIApplicationDelegate, RCTBridgeDelegate {

  var window: UIWindow?

  func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
  ) -> Bool {

    let bridge = RCTBridge(delegate: self, launchOptions: launchOptions)

    let rootView = RCTRootView(
      bridge: bridge!,
      moduleName: "turbodrivermobile",
      initialProperties: nil
    )

    rootView.backgroundColor = UIColor.white

    let rootViewController = UIViewController()
    rootViewController.view = rootView

    window = UIWindow(frame: UIScreen.main.bounds)
    window?.rootViewController = rootViewController
    window?.makeKeyAndVisible()

    return true
  }

  // 🔥 FIXED: explicit bundle root for monorepo layout
  func sourceURL(for bridge: RCTBridge) -> URL {
#if DEBUG
    let url = RCTBundleURLProvider.sharedSettings()
      .jsBundleURL(
        forBundleRoot: "mobile/index",
        fallbackResource: nil
      )

    if url == nil {
      fatalError("❌ Metro bundle URL is nil. Is Metro running from /mobile?")
    }

    return url!
#else
    return Bundle.main.url(forResource: "main", withExtension: "jsbundle")!
#endif
  }
}
