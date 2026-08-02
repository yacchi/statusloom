// Captures the README's screenshots and the editing animation by driving the
// real configurator in Chromium.
//
// Run it through scripts/capture-media.sh (or `mise run capture-media`), which
// starts a configurator against an ISOLATED config and passes UI_URL — this
// script must never be pointed at a configurator serving the developer's own
// configuration, since it edits the document it is given.
//
// Output: docs/media/*.png plus editor.gif. Both are committed (they are README
// content, not build artifacts).
//
// Requires: playwright + its chromium, and ffmpeg for the GIF.

import { chromium, type Page } from "playwright";
import { execFileSync } from "node:child_process";
import { mkdirSync, readdirSync, rmSync } from "node:fs";
import { join } from "node:path";

const UI_URL = process.env.UI_URL;
const OUT = process.env.MEDIA_OUT ?? "docs/media";
// "anim" records the editing clip, "stills" takes the screenshots. They run
// against SEPARATE configurator instances (see capture-media.sh) because each
// one edits the document: sharing a store made the recording start from the
// state the stills had built, and the drag then landed in the wrong row.
const PHASE = process.env.CAPTURE_PHASE ?? "all";
if (!UI_URL) {
    throw new Error("UI_URL is required (set by scripts/capture-media.sh)");
}

// Wide enough that the preview controls stay on one line and the sample status
// line does not soft-wrap (both look like defects in a still), at 2x for crisp
// text on high-DPI displays.
const VIEWPORT = { width: 1600, height: 1000 };
const SCALE = 2;
// Narrower than the editor, so the sample lines fit without wrapping.
const PREVIEW_COLUMNS = "100";

function sh(cmd: string, args: string[]): void {
    execFileSync(cmd, args, { stdio: "inherit" });
}

// Adds a visible cursor dot to the page: Playwright moves the real mouse but
// screenshots and videos never show a pointer, which makes a drag recording
// impossible to follow.
async function installCursor(page: Page): Promise<void> {
    await page.addStyleTag({
        content: `
        #__cursor {
            position: fixed;
            z-index: 2147483647;
            width: 14px;
            height: 14px;
            margin: -7px 0 0 -7px;
            border-radius: 50%;
            background: rgba(255, 255, 255, 0.9);
            box-shadow: 0 0 0 2px rgba(0, 0, 0, 0.55), 0 2px 8px rgba(0, 0, 0, 0.5);
            pointer-events: none;
            transition: transform 0.04s linear;
        }`,
    });
    await page.evaluate(() => {
        const dot = document.createElement("div");
        dot.id = "__cursor";
        document.body.appendChild(dot);
        window.addEventListener(
            "mousemove",
            (e) => {
                dot.style.left = `${e.clientX}px`;
                dot.style.top = `${e.clientY}px`;
            },
            true,
        );
    });
}

// Moves the mouse in steps so a recording shows travel rather than teleporting.
async function glide(page: Page, to: { x: number; y: number }): Promise<void> {
    await page.mouse.move(to.x, to.y, { steps: 24 });
}

async function centerOf(page: Page, selector: string): Promise<{ x: number; y: number }> {
    const b = await page.locator(selector).first().boundingBox();
    if (!b) {
        throw new Error(`no box for ${selector}`);
    }
    return { x: b.x + b.width / 2, y: b.y + b.height / 2 };
}

async function captureStills(): Promise<void> {
    const browser = await chromium.launch();
    const still = await browser.newPage({ viewport: VIEWPORT, deviceScaleFactor: SCALE });
    await still.goto(UI_URL!, { waitUntil: "networkidle" });
    await still.waitForSelector(".seg-chip", { timeout: 20000 });
    await still.locator('[data-testid="width-slider"]').fill(PREVIEW_COLUMNS);
    await still.waitForTimeout(900);
    await still.screenshot({ path: `${OUT}/configurator.png` });

    // A selected chip with its properties open: the direct-manipulation story.
    await still.locator(".row-track .seg-chip").nth(2).click();
    await still.waitForTimeout(700);
    await still.screenshot({ path: `${OUT}/properties.png` });

    // The width-adaptive container with a condition on a variant.
    await still.locator("button", { hasText: "+ Add responsive" }).first().click();
    // Wait for the AST ids to come back from serialize+parse: right after the
    // click the new variant is still `pending` and its testid has an empty id
    // ("variant-when--add"), so reading the prefix too early yields nothing.
    await still.waitForSelector('[data-testid^="variant-when-L"][data-testid$="-add"]', {
        timeout: 15000,
    });
    const add = still.locator('[data-testid^="variant-when-L"][data-testid$="-add"]').first();
    const prefix = (await add.getAttribute("data-testid"))!.replace(/-add$/, "");
    await add.click();
    await still.waitForSelector(`[data-testid="${prefix}-metric"]`);
    await still.selectOption(`[data-testid="${prefix}-metric"]`, "account-type");
    await still.waitForTimeout(700);
    await still.locator(".canvas-panel").screenshot({ path: `${OUT}/responsive.png` });

    // The DSL source of what was just built visually.
    await still.locator('[data-testid="view-split"]').click();
    await still.waitForTimeout(800);
    await still.screenshot({ path: `${OUT}/dsl-editor.png` });

    // Version history: every save is a revision, so it needs a couple of saves
    // with an edit in between to have anything to show.
    await still.locator('[data-testid="view-visual"]').click();
    await still.waitForTimeout(400);
    await still.locator('[data-testid="save-button"]').click();
    await still.waitForTimeout(1200);
    await still.locator('[data-testid="palette-field:git-branch"]').click();
    await still.waitForTimeout(900);
    await still.locator('[data-testid="save-button"]').click();
    await still.waitForTimeout(1200);
    await still.locator('[data-testid="history-button"]').click();
    await still.waitForSelector('[data-testid="history-list"]', { timeout: 10000 });
    await still.waitForTimeout(700);
    // Select the older revision: the diff against the current document is the
    // point of the panel, and an unselected panel is a blank right-hand pane.
    await still.locator('[data-testid="history-list"] li').nth(1).click();
    await still.waitForSelector('[data-testid="history-diff"]', { timeout: 10000 });
    await still.waitForTimeout(700);
    await still.locator(".modal").screenshot({ path: `${OUT}/history.png` });
    await still.locator('[data-testid="modal-close"]').click();
    await still.waitForTimeout(400);

    // The live monitor: it renders the status line from REAL sessions as they
    // update, so with no session captured yet it shows the command to run.
    await still.locator("button", { hasText: "Start live monitor" }).first().click();
    await still.waitForTimeout(1500);
    await still.screenshot({ path: `${OUT}/live-monitor.png` });

    // No screenshot of the embedded terminal: it starts Claude Code itself, and
    // in the throwaway config directory this capture must use, Claude Code has no
    // credentials — it walks from the theme picker straight to an OAuth sign-in
    // URL. Capturing that would show a login page (and a live auth URL) instead
    // of the feature. Authenticating would mean pointing the capture at the
    // developer's real config, which this script must never do. The feature is
    // described in the README instead.

    await still.close();
    await browser.close();
    console.log(
        `wrote ${OUT}/{configurator,properties,responsive,dsl-editor,history,live-monitor}.png`,
    );
}

async function captureAnimation(): Promise<void> {
    const videoDir = join(OUT, ".video");
    rmSync(videoDir, { recursive: true, force: true });
    mkdirSync(videoDir, { recursive: true });

    const browser = await chromium.launch();
    const rec = await browser.newContext({
        viewport: VIEWPORT,
        recordVideo: { dir: videoDir, size: VIEWPORT },
    });
    const page = await rec.newPage();
    await page.goto(UI_URL!, { waitUntil: "networkidle" });
    await page.waitForSelector(".seg-chip", { timeout: 20000 });
    await page.locator('[data-testid="width-slider"]').fill(PREVIEW_COLUMNS);
    await page.waitForTimeout(700);
    await installCursor(page);
    await page.mouse.move(VIEWPORT.width / 2, VIEWPORT.height / 2);
    await page.waitForTimeout(900);

    // The clip shows three things, in the order a user meets them: dragging a
    // field in from the palette, restyling it, and watching the line adapt to the
    // terminal width. The drag is the point — it is what a TUI-style editor
    // cannot offer — so the sequence below asserts that it actually started
    // rather than risking a recording that silently shows a failed drag.

    // 1. Drag a field from the palette onto a chip in line 1. The moves are
    //    PACED (a pause after each): a single steps-only move works too, but
    //    pacing gives the recording visible travel and the drop caret time to
    //    paint at each position.
    const src = await centerOf(page, '[data-testid="palette-field:git-branch"]');
    const dst = await centerOf(page, '[data-testid="seg-0-4"]');
    await glide(page, src);
    await page.waitForTimeout(400);
    await page.mouse.down();
    await page.waitForTimeout(120);
    // Past dnd-kit's 4px activation distance, still inside the palette, so the
    // clip shows where the chip is coming from.
    await page.mouse.move(src.x + 10, src.y + 4);
    await page.waitForTimeout(150);
    await page.waitForSelector(".drag-overlay-chip", { timeout: 5000 });
    const STEPS = 14;
    for (let i = 1; i <= STEPS; i += 1) {
        await page.mouse.move(
            src.x + ((dst.x - src.x) * i) / STEPS,
            src.y + ((dst.y - src.y) * i) / STEPS,
        );
        await page.waitForTimeout(45);
    }
    // Hold on the target so the drop caret is legible before releasing.
    await page.waitForSelector(".seg-chip.drop-before, .seg-chip.drop-after", { timeout: 5000 });
    // Which side of the target the caret is on decides the dropped chip's index,
    // which is how step 2 finds it (node ids are positional, so they all shift).
    const dropsAfter = (await page.locator('[data-testid="seg-0-4"].drop-after').count()) > 0;
    await page.waitForTimeout(600);
    await page.mouse.up();
    await page.waitForTimeout(1300);

    // 2. Select the dropped chip and recolor it — the preview restyles in place.
    const chip = page
        .locator(".row-track")
        .first()
        .locator(".seg-chip")
        .nth(dropsAfter ? 5 : 4);
    const chipBox = await chip.boundingBox();
    if (chipBox) {
        await glide(page, { x: chipBox.x + chipBox.width / 2, y: chipBox.y + chipBox.height / 2 });
    }
    await chip.click();
    await page.waitForTimeout(1000);
    // A named ANSI color, so the recolor is obvious against the default text.
    const swatch = page.locator('[data-testid="swatch-cyan"]').first();
    if ((await swatch.count()) > 0) {
        const b = await swatch.boundingBox();
        if (b) {
            await glide(page, { x: b.x + b.width / 2, y: b.y + b.height / 2 });
            await page.waitForTimeout(250);
            await swatch.click();
            await page.waitForTimeout(1200);
        }
    }

    // 3. Narrow the terminal: the row soft-wraps in the editor and the fields
    //    switch to their compact form in the preview.
    const slider = page.locator('[data-testid="width-slider"]');
    const sb = await slider.boundingBox();
    if (sb) {
        await glide(page, { x: sb.x + sb.width * 0.6, y: sb.y + sb.height / 2 });
        await page.mouse.down();
        await glide(page, { x: sb.x + sb.width * 0.32, y: sb.y + sb.height / 2 });
        await page.waitForTimeout(800);
        await glide(page, { x: sb.x + sb.width * 0.1, y: sb.y + sb.height / 2 });
        await page.mouse.up();
        await page.waitForTimeout(1600);
    }

    await page.close();
    await rec.close();
    await browser.close();

    await webmToGif("editor.gif");
}

// Turns the recording Playwright just wrote into the GIF the README embeds, and
// removes the raw video. Two-pass palette at a reduced size and frame rate: the
// UI is flat colour, so it survives that and a README GIF has to stay small.
async function webmToGif(name: string): Promise<void> {
    const videoDir = join(OUT, ".video");
    const webm = readdirSync(videoDir).find((f) => f.endsWith(".webm"));
    if (!webm) {
        throw new Error("playwright wrote no video");
    }
    const src = join(videoDir, webm);
    const palette = join(videoDir, "palette.png");
    const gifFilter = "fps=10,scale=900:-1:flags=lanczos";
    sh("ffmpeg", ["-y", "-loglevel", "error", "-i", src,
        "-vf", `${gifFilter},palettegen=max_colors=64:stats_mode=diff`, palette]);
    sh("ffmpeg", ["-y", "-loglevel", "error", "-i", src, "-i", palette,
        "-lavfi", `${gifFilter}[v];[v][1:v]paletteuse=dither=bayer:bayer_scale=3`,
        `${OUT}/${name}`]);
    rmSync(videoDir, { recursive: true, force: true });
    console.log(`wrote ${OUT}/${name}`);
}

// The grouping clip: what a <span> buys you. Selects the group (not a field),
// restyles it so the whole group changes at once, edits the group's own label,
// then overrides one child to show that inheritance is nearest-wins.
async function captureSpanClip(): Promise<void> {
    const videoDir = join(OUT, ".video");
    rmSync(videoDir, { recursive: true, force: true });
    mkdirSync(videoDir, { recursive: true });

    const browser = await chromium.launch();
    const rec = await browser.newContext({
        viewport: VIEWPORT,
        recordVideo: { dir: videoDir, size: VIEWPORT },
    });
    const page = await rec.newPage();
    await page.goto(UI_URL!, { waitUntil: "networkidle" });
    await page.waitForSelector(".span-group", { timeout: 20000 });
    await page.locator('[data-testid="width-slider"]').fill(PREVIEW_COLUMNS);
    await page.waitForTimeout(700);
    await installCursor(page);
    await page.mouse.move(VIEWPORT.width / 2, VIEWPORT.height / 2);
    await page.waitForTimeout(900);

    // 1. Select the GROUP via its grip — a span with no decoration of its own
    //    would otherwise have no clickable area.
    const grip = page.locator(".span-handle").first();
    const gb = await grip.boundingBox();
    if (gb) {
        await glide(page, { x: gb.x + gb.width / 2, y: gb.y + gb.height / 2 });
    }
    await grip.click();
    await page.waitForTimeout(1100);

    // 2. One colour for the whole group: prefix, both fields, separator.
    const swatch = page.locator('[data-testid="swatch-magenta"]').first();
    if ((await swatch.count()) > 0) {
        const b = await swatch.boundingBox();
        if (b) {
            await glide(page, { x: b.x + b.width / 2, y: b.y + b.height / 2 });
            await page.waitForTimeout(250);
            await swatch.click();
            await page.waitForTimeout(1400);
        }
    }

    // 3. The group's own label, which disappears with the group.
    const prefix = page.locator('[data-testid="attr-prefix"]');
    if ((await prefix.count()) > 0) {
        const b = await prefix.boundingBox();
        if (b) {
            await glide(page, { x: b.x + b.width / 2, y: b.y + b.height / 2 });
        }
        await prefix.click();
        await prefix.fill("");
        await prefix.type("5h left: ", { delay: 110 });
        await page.waitForTimeout(1300);
    }

    // 4. Override one child: nearest-wins, so the group's colour stays on
    //    everything else.
    const inner = page.locator(".span-group .seg-chip.inner").first();
    if ((await inner.count()) > 0) {
        const b = await inner.boundingBox();
        if (b) {
            await glide(page, { x: b.x + b.width / 2, y: b.y + b.height / 2 });
        }
        await inner.click();
        await page.waitForTimeout(900);
        const yellow = page.locator('[data-testid="swatch-yellow"]').first();
        if ((await yellow.count()) > 0) {
            const yb = await yellow.boundingBox();
            if (yb) {
                await glide(page, { x: yb.x + yb.width / 2, y: yb.y + yb.height / 2 });
                await page.waitForTimeout(250);
                await yellow.click();
                await page.waitForTimeout(1600);
            }
        }
    }

    await page.close();
    await rec.close();
    await browser.close();
    await webmToGif("span-grouping.gif");
}

mkdirSync(OUT, { recursive: true });
if (PHASE === "stills" || PHASE === "all") {
    await captureStills();
}
if (PHASE === "anim" || PHASE === "all") {
    await captureAnimation();
}
if (PHASE === "span" || PHASE === "all") {
    await captureSpanClip();
}
