// One page for both windows: the panel loads /panel, the main window /.
if (location.pathname.startsWith("/panel")) {
  document.documentElement.classList.add("panel-page");
  void import("./panel").then((m) => m.start());
} else {
  void import("./app").then((m) => m.start());
}
