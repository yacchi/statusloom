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
    await still.close();
    await browser.close();
    console.log(`wrote ${OUT}/{configurator,properties,responsive,dsl-editor}.png`);
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

    // The clip shows three things, in the order a user meets them: adding a
    // field, restyling it, and watching the line adapt to the terminal width.
    //
    // It clicks rather than drags. Synthetic pointer sequences from Playwright
    // do not reliably start a dnd-kit drag (the palette chip never enters its
    // dragging state), and a recording that silently fails to drag is worse than
    // one that shows the click path — which is a real affordance, not a stand-in:
    // clicking a palette chip appends it to the active line.

    // 1. Add a field: the preview grows a chip and the DSL behind it changes.
    const src = await centerOf(page, '[data-testid="palette-field:git-branch"]');
    await glide(page, src);
    await page.waitForTimeout(400);
    await page.locator('[data-testid="palette-field:git-branch"]').click();
    await page.waitForTimeout(1200);

    // 2. Select the new chip and recolor it — the preview restyles in place.
    const chip = page.locator(".row-track").first().locator(".seg-chip").last();
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

    // ---- webm -> gif ------------------------------------------------------
    const videoDir2 = join(OUT, ".video");
    const webm = readdirSync(videoDir2).find((f) => f.endsWith(".webm"));
    if (!webm) {
        throw new Error("playwright wrote no video");
    }
    const src_webm = join(videoDir2, webm);
    // Two-pass palette for a readable GIF at a sane size: the UI is mostly flat
    // colors, so a per-clip palette keeps text crisp.
    const palette = join(videoDir2, "palette.png");
    const gifFilter = "fps=10,scale=900:-1:flags=lanczos";
    sh("ffmpeg", ["-y", "-loglevel", "error", "-i", src_webm,
        "-vf", `${gifFilter},palettegen=max_colors=64:stats_mode=diff`, palette]);
    sh("ffmpeg", ["-y", "-loglevel", "error", "-i", src_webm, "-i", palette,
        "-lavfi", `${gifFilter}[v];[v][1:v]paletteuse=dither=bayer:bayer_scale=3`,
        `${OUT}/editor.gif`]);
    // Keep the source recording out of git; the GIF is the deliverable.
    rmSync(videoDir2, { recursive: true, force: true });
    console.log(`wrote ${OUT}/editor.gif`);
}

mkdirSync(OUT, { recursive: true });
if (PHASE === "stills" || PHASE === "all") {
    await captureStills();
}
if (PHASE === "anim" || PHASE === "all") {
    await captureAnimation();
}
