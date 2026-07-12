import os
import platform
import tempfile
from typing import Optional

from textual import on
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Container, Horizontal, Vertical
from textual.screen import Screen
from textual.widgets import Button, DataTable, Footer, Header, Label, ProgressBar, Static, Tabs, Tab

from .gh import (
    CommitArtifact, GitHub, Release,
    binary_name, detect_os_arch, is_in_path, suggest_install_path,
)

RELEASES_TAB = "Releases"
COMMITS_TAB = "Commit Builds"
OS_NAME, ARCH = detect_os_arch()
MATCHING_BINARY = binary_name(OS_NAME, ARCH)


class DownloadScreen(Screen):
    def __init__(self, label: str, total_steps: int = 1):
        super().__init__()
        self._label = label
        self._total = total_steps
        self.progress: Optional[ProgressBar] = None
        self._completed = False

    def compose(self) -> ComposeResult:
        yield Header()
        yield Vertical(
            Label(self._label, id="download-label"),
            ProgressBar(total=self._total, show_eta=False, id="download-progress"),
            Label("", id="download-status"),
            id="download-container",
        )
        yield Footer()

    def advance(self, delta: float = 1):
        if self.progress is None:
            self.progress = self.query_one("#download-progress", ProgressBar)
        self.progress.advance(delta)
        pct = int(self.progress.percentage * 100)
        self.query_one("#download-status", Label).update(f"{pct}%")

    def complete(self, msg: str = "Done!"):
        self._completed = True
        self.query_one("#download-status", Label).update(msg)

    @property
    def is_done(self) -> bool:
        return self._completed


class InstallerApp(App):
    TITLE = "Tengiz Installer"
    SUB_TITLE = f"{OS_NAME}/{ARCH}"
    CSS = """
    Screen {
        layout: vertical;
    }
    #main-container {
        height: 1fr;
        padding: 1;
    }
    #os-info {
        text-align: right;
        padding: 0 1;
        color: $text-muted;
    }
    DataTable {
        height: 1fr;
        margin: 1 0;
    }
    #action-bar {
        height: auto;
        dock: bottom;
        padding: 1;
        background: $surface;
    }
    #status-line {
        height: auto;
        padding: 0 1;
        color: $text-muted;
    }
    #action-buttons {
        height: auto;
        align: center middle;
    }
    Button {
        margin: 0 1;
    }
    #download-container {
        align: center middle;
        padding: 2;
    }
    #download-label {
        text-align: center;
        margin-bottom: 1;
    }
    #download-progress {
        width: 40;
    }
    #download-status {
        text-align: center;
        margin-top: 1;
        color: $text-muted;
    }
    Tabs {
        margin: 1 0 0 0;
    }
    """

    BINDINGS = [
        Binding("q", "quit", "Quit"),
        Binding("r", "refresh", "Refresh"),
    ]

    def __init__(self):
        super().__init__()
        self.gh = GitHub()
        self.releases: list[Release] = []
        self.artifacts: list[CommitArtifact] = []
        self.selected_release: Optional[Release] = None
        self.selected_artifact: Optional[CommitArtifact] = None
        self.current_tab = RELEASES_TAB
        self.downloaded_binary: Optional[str] = None

    def compose(self) -> ComposeResult:
        yield Header()
        yield Container(
            Tabs(Tab(RELEASES_TAB, id="tab-releases"), Tab(COMMITS_TAB, id="tab-commits"), id="tabs"),
            DataTable(id="table", cursor_type="row"),
            id="main-container",
        )
        yield Container(
            Static("", id="status-line"),
            Horizontal(
                Button("Download", id="btn-download", variant="primary"),
                Button("Install", id="btn-install", variant="success"),
                Button("Refresh", id="btn-refresh"),
                id="action-buttons",
            ),
            id="action-bar",
        )
        yield Footer()

    def on_mount(self):
        table = self.query_one("#table", DataTable)
        table.add_columns("Version", "Info", "Date", "Type")
        self.load_releases()
        self.update_status()

    def update_status(self, msg: str = ""):
        status = self.query_one("#status-line", Static)
        if self.current_tab == RELEASES_TAB and self.selected_release:
            assets = [a for a in self.selected_release.assets if MATCHING_BINARY in a.name]
            if assets:
                a = assets[0]
                size_mb = a.size / (1024 * 1024)
                info = f"Selected: {self.selected_release.tag} → {a.name} ({size_mb:.1f} MB)"
            else:
                info = f"Selected: {self.selected_release.tag} (no binary for {OS_NAME}/{ARCH})"
        elif self.current_tab == COMMITS_TAB and self.selected_artifact:
            size_mb = self.selected_artifact.size / (1024 * 1024)
            info = f"Selected: {self.selected_artifact.head_sha[:8]} → {self.selected_artifact.name} ({size_mb:.1f} MB)"
        else:
            info = "Select a version to download"
        if msg:
            info = f"{info} | {msg}"
        status.update(info)

    def load_releases(self):
        table = self.query_one("#table", DataTable)
        table.clear()
        self.set_loading(True)

        def done(fut):
            self.set_loading(False)
            try:
                self.releases = fut.result()
                for r in self.releases:
                    label = "stable"
                    if r.prerelease:
                        label = "pre-release"
                    date = r.published_at[:10] if r.published_at else ""
                    table.add_row(r.tag, r.name or r.tag, date, label)
                if self.releases:
                    table.move_cursor(row=0)
                    self.on_table_row_selected(table, table.coordinate_column_index)
            except Exception as e:
                self.update_status(f"Error: {e}")

        import asyncio
        asyncio.ensure_future(self.gh.get_releases()).add_done_callback(done)

    def load_artifacts(self):
        table = self.query_one("#table", DataTable)
        table.clear()
        self.set_loading(True)

        def done(fut):
            self.set_loading(False)
            try:
                self.artifacts = fut.result()
                if not self.artifacts:
                    table.add_row("—", "No commit builds available", "", "")
                    table.add_row("", "Install gh CLI or set GH_TOKEN", "", "")
                    self.update_status("No commit builds (auth required)")
                    return
                for a in self.artifacts:
                    label = "expired" if a.expired else "active"
                    date = a.created_at[:10] if a.created_at else ""
                    table.add_row(a.head_sha[:8], a.head_branch, date, label)
                if self.artifacts:
                    table.move_cursor(row=0)
                    self.on_table_row_selected(table, table.coordinate_column_index)
            except Exception as e:
                self.update_status(f"Error: {e}")

        import asyncio
        asyncio.ensure_future(self.gh.get_artifacts()).add_done_callback(done)

    def set_loading(self, loading: bool):
        table = self.query_one("#table", DataTable)
        if loading:
            table.clear()
            table.add_row("Loading...", "", "", "")

    @on(Tabs.TabActivated)
    def on_tab_changed(self, event: Tabs.TabActivated):
        self.current_tab = event.tab.label
        if self.current_tab == RELEASES_TAB:
            self.load_releases()
        else:
            self.load_artifacts()
        self.update_status()

    @on(DataTable.RowSelected)
    def on_table_row_selected(self, table: DataTable, row_key):
        if row_key is None:
            row_key = table.coordinate_column_index
        if not self.releases and not self.artifacts:
            return
        try:
            idx = table.cursor_row
        except Exception:
            return

        if self.current_tab == RELEASES_TAB:
            if 0 <= idx < len(self.releases):
                self.selected_release = self.releases[idx]
                self.selected_artifact = None
        else:
            if 0 <= idx < len(self.artifacts):
                self.selected_artifact = self.artifacts[idx]
                self.selected_release = None
        self.update_status()

    def action_refresh(self):
        self.downloaded_binary = None
        if self.current_tab == RELEASES_TAB:
            self.load_releases()
        else:
            self.load_artifacts()

    @on(Button.Pressed, "#btn-refresh")
    def on_refresh(self):
        self.action_refresh()

    @on(Button.Pressed, "#btn-download")
    def on_download(self):
        if self.current_tab == RELEASES_TAB:
            self._download_release()
        else:
            self._download_artifact()

    def _download_release(self):
        if not self.selected_release:
            self.update_status("No release selected")
            return
        assets = [a for a in self.selected_release.assets if MATCHING_BINARY in a.name]
        if not assets:
            self.update_status(f"No binary for {OS_NAME}/{ARCH} in this release")
            return
        asset = assets[0]

        tmp = tempfile.mkdtemp(prefix="tengiz-")
        dest = os.path.join(tmp, MATCHING_BINARY)

        def run():
            async def inner():
                screen = DownloadScreen(f"Downloading {asset.name}...")
                await self.push_screen(screen)

                def on_progress(pct):
                    self.call_from_thread(screen.advance, pct)

                try:
                    await self.gh.download_release_asset(asset.url, dest, on_progress)
                    screen.complete("Download complete!")
                    self.downloaded_binary = dest
                    self.update_status(f"Downloaded {asset.name}")
                except Exception as e:
                    screen.complete(f"Error: {e}")
                    self.update_status(f"Download failed: {e}")

            import asyncio
            asyncio.ensure_future(inner())

        run()

    def _download_artifact(self):
        if not self.selected_artifact:
            self.update_status("No commit build selected")
            return
        if self.selected_artifact.expired:
            self.update_status("This artifact has expired")
            return

        tmp = tempfile.mkdtemp(prefix="tengiz-")

        def run():
            async def inner():
                screen = DownloadScreen(f"Downloading {self.selected_artifact.name}...")
                await self.push_screen(screen)

                def on_progress(pct):
                    self.call_from_thread(screen.advance, pct * 0.8)

                try:
                    extracted = await self.gh.download_and_extract_artifact(
                        self.selected_artifact.id, MATCHING_BINARY, tmp, on_progress,
                    )
                    if extracted:
                        screen.complete("Download complete!")
                        self.downloaded_binary = extracted
                        self.update_status(f"Downloaded {self.selected_artifact.name}")
                    else:
                        screen.complete("Extraction failed")
                        self.update_status("Failed to extract binary from artifact")
                except Exception as e:
                    screen.complete(f"Error: {e}")
                    self.update_status(f"Download failed: {e}")

            import asyncio
            asyncio.ensure_future(inner())

        run()

    @on(Button.Pressed, "#btn-install")
    def on_install(self):
        if not self.downloaded_binary or not os.path.exists(self.downloaded_binary):
            self.update_status("Download a binary first")
            return

        dest = suggest_install_path()
        parent = os.path.dirname(dest)

        try:
            self.gh.install_binary(self.downloaded_binary, dest)
            in_path = is_in_path(parent)
            msg = f"Installed to {dest}"
            if not in_path:
                msg += f" (add {parent} to PATH)"
            self.update_status(msg)
            self.notify(
                f"Tengiz installed to {dest}",
                title="Installation complete",
                timeout=5,
            )
        except Exception as e:
            self.update_status(f"Install failed: {e}")
