# YourPet

YourPet is a small, animated desktop companion for Windows 11. It runs locally as a portable app. The included pet, Ryzhik, has three idle poses, walks on its own, jumps, and turns its head toward the cursor.

## Install on Windows 11

1. Download the ZIP from [GitHub Releases](https://github.com/timkimtim/YourPet/releases/latest). Choose **x64** for an Intel or AMD PC. Choose **ARM64** for a Windows on Arm PC. Windows shows the system type under **Settings → System → About**.
2. Extract the ZIP into a folder you intend to keep, such as `Documents\YourPet`.
3. Double-click `YourPet.exe`. Ryzhik appears near a corner of the screen, and the YourPet icon appears in the notification area next to the clock. Check the overflow arrow if the icon is hidden.

YourPet runs without an installer, an account, or administrator permissions. Release binaries are unsigned. Windows may show a publisher prompt for the downloaded executable.

## Controls

- **Drag the pet** with the left mouse button to change its position. **Double-click** to play its jump animation.
- **Right-click** the pet or the tray icon for its menu. Use **Animations** to preview individual clips, or **Change idle pose** to switch between the available resting poses.
- **Auto-walk** lets the pet stroll around the current monitor during quiet moments.
- **Gaze radius** sets how close the cursor needs to be before the pet turns its head. Move the cursor around the pet to see the direction frames.
- **Size** scales the artwork. **Always on top** controls whether the pet stays above ordinary windows.
- **Click-through** sends mouse clicks to the window underneath. Use the tray icon to turn the mode off when you want to drag or double-click the pet.
- **Start with Windows** launches YourPet when you sign in. Enable it after moving the executable to its permanent folder.
- Use **Hide pet**, **Show pet**, **Move to screen corner**, or **Exit** from the tray menu.

The app saves preferences and a copy of the imported package in `%APPDATA%\YourPet`. To remove the app, turn off **Start with Windows**, choose **Exit**, and delete the extracted folder. You can also delete `%APPDATA%\YourPet` to remove the saved pet and preferences.

## Load a pet

1. Get a `.pet` package or make one using the guide below.
2. Right-click the pet or the tray icon and choose **Load pet…**.
3. Select the `.pet` or `.zip` file. The new pet appears and remains selected the next time you launch YourPet.
4. Choose **Restore default pet** to go back to Ryzhik.

Try the editable [Ryzhik example package](cmd/yourpet/default.pet). Open it as a ZIP to see how its animation manifest maps to the sprite sheet.

## Create a pet package

A `.pet` file is a regular ZIP archive. Its root contains the manifest and the transparent PNG named by that manifest.

```text
my-pet.pet
├── pet.json
└── spritesheet.png
```

### 1. Create the sprite sheet

- Draw or export the frames into a regular grid. Every cell has the same width and height between 16 and 320 pixels. Keep the complete sheet at or below 4096 × 4096 pixels and save it as a transparent PNG.
- Keep the character at a consistent scale. In grounded poses, align the feet or lowest contact point across the frames. Preserve the empty space under the pet in airborne frames.
- Make a separate clip for each idle pose, such as sitting, standing, and sleeping. Each clip loops during its assigned stay. Give each action, such as jumping, its own sequence and timing.
- For autonomous walking and animated dragging, draw separate left-facing and right-facing clips. Verify that the frames are in playback order.
- To make the pet follow the cursor, draw 4, 8, or 16 head poses in clockwise order, beginning at straight up. Keep the body anchored as the face turns. The regular idle frame supplies the neutral pose.
- The cell coordinates are zero-based. `"col": 0, "row": 0` is the top-left cell. The first rightward pose is index one quarter of the way through the direction list.
- You can reuse an existing transparent Codex-format atlas. Set the cell size to **192 × 208** and reference each populated cell. In a v2 atlas the first nine rows are action clips and rows 9 and 10 hold the 16 directions. Add separate idle clips using the frames you have drawn for them.

### 2. Write `pet.json`

Save it as UTF-8. This minimal runnable example uses a sitting idle in the first two cells of row 0 and a sleeping idle in row 1. Change the indices and timing to match your own sheet.

```json
{
  "version": 1,
  "name": "My Pet",
  "atlas": "spritesheet.png",
  "cellWidth": 160,
  "cellHeight": 176,
  "animations": [
    {
      "id": "sit",
      "label": "Sit",
      "frames": [
        { "col": 0, "row": 0, "ms": 1400 },
        { "col": 1, "row": 0, "ms": 140 }
      ]
    },
    {
      "id": "sleep",
      "label": "Sleep",
      "frames": [
        { "col": 0, "row": 1, "ms": 650 },
        { "col": 1, "row": 1, "ms": 650 }
      ]
    }
  ],
  "idle": [
    { "animation": "sit", "minMs": 6000, "maxMs": 10000 },
    { "animation": "sleep", "minMs": 10000, "maxMs": 16000 }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `animations` | One to 32 named clips. Each has a unique `id`, a menu `label`, and one to 64 frames. |
| `frames[].ms` | How long to show a frame, from 40 to 30,000 milliseconds. |
| `idle` | One to 12 distinct clip IDs. The first entry is the starting idle. After a random stay between `minMs` and `maxMs`, YourPet selects another available idle. A stay can be 500 to 300,000 milliseconds. |
| `walk` | Optional object with `left` and `right` set to two different clip IDs. This enables auto-walk and directional animation during dragging. |
| `jump` | Optional ID of the clip to play on double-click. |
| `look` | Optional head-turn settings. `neutral` names an idle clip. `anchor` holds the x and y coordinates of the gaze origin inside a cell. `directions` holds cells in clockwise order, from straight up. |

For example, once your sheet has four direction poses in row 2 and you have defined clips named `walk-left`, `walk-right`, and `jump`, add these top-level fields.

```json
{
  "walk": { "left": "walk-left", "right": "walk-right" },
  "jump": "jump",
  "look": {
    "neutral": "sit",
    "anchor": [80, 84],
    "directions": [
      { "col": 0, "row": 2 },
      { "col": 1, "row": 2 },
      { "col": 2, "row": 2 },
      { "col": 3, "row": 2 }
    ]
  }
}
```

The four directions above are up, right, down, and left. An eight-direction sequence adds diagonals between them. A sixteen-direction sequence uses 22.5° steps. Cursor movement advances through the authored poses, and the named neutral idle supplies the face when the cursor is directly over the gaze origin.

### 3. ZIP, load, and test

1. In Explorer, select `pet.json` and `spritesheet.png` together and compress them into a ZIP. Put the files at the root of the archive.
2. Keep the archive under 24 MB. You can leave the extension as `.zip` or rename it to `.pet`.
3. Import it via **Load pet…**. Test each clip using **Animations**, then try the gaze radius, double-click, and auto-walk.
4. To revise the pet, edit the source files, make a new ZIP, and load the new ZIP. YourPet uses its saved copy of the imported package.

If the loader reports a blank frame, check the referenced cell coordinates and the PNG alpha channel. If the pet jitters, check the cell registration across the clip. If the pet cannot look around, check the direction count, the clockwise order, and the gaze-radius setting.

## Build from source

Use Go 1.22 or later on Windows.

```powershell
go test ./...
go build -trimpath -ldflags "-s -w -H=windowsgui" -o YourPet.exe ./cmd/yourpet
```

The default build embeds Ryzhik from `cmd/yourpet/default.pet`. To ship a build with your own starting pet, replace that file with your package, then run the build command again. The app can still import other packages at runtime.

The Windows icon source and resource configuration are in `cmd/yourpet/winres`. If you change them, regenerate the resource objects from the repository root.

```powershell
go install github.com/tc-hib/go-winres@v0.3.3
go-winres make --in cmd/yourpet/winres/winres.json --out cmd/yourpet/rsrc --arch amd64,arm64
```
