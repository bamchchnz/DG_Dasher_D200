#!/bin/bash
# Signs "Dasher D200.app" with your own "Dasher D200 Local" certificate so
# that macOS keeps its Local Network permission across updates.
# Run it after installing or updating the app:
#   double-click this file, or in Terminal:  bash "Sign Dasher D200.command"
# Optional: give the app's path as an argument if it isn't in /Applications.

CERT="Dasher D200 Local"
APP="${1:-/Applications/Dasher D200.app}"

echo "Dasher D200 signing helper"
echo

if [ ! -d "$APP" ]; then
    echo "Can't find the app at: $APP"
    echo "Copy Dasher D200.app to Applications first, or run:"
    echo "  bash \"$0\" \"/path/to/Dasher D200.app\""
    read -r -p "Press Return to close." _
    exit 1
fi

if ! security find-certificate -c "$CERT" >/dev/null 2>&1; then
    echo "No certificate named \"$CERT\" was found in your keychain."
    echo "Create one first: Keychain Access > Certificate Assistant >"
    echo "Create a Certificate... Name: $CERT, Identity Type: Self-Signed Root,"
    echo "Certificate Type: Code Signing."
    read -r -p "Press Return to close." _
    exit 1
fi

# Clear the "downloaded from the internet" flag so Gatekeeper doesn't block it.
xattr -dr com.apple.quarantine "$APP" 2>/dev/null

echo "Signing $APP ..."
echo "(If macOS asks whether codesign may use your key, click Always Allow.)"
if ! codesign --force --deep --sign "$CERT" "$APP"; then
    echo
    echo "Signing failed - see the message above."
    read -r -p "Press Return to close." _
    exit 1
fi

if codesign --verify --deep "$APP" 2>/dev/null; then
    echo
    echo "Done. The app is signed with \"$CERT\"."
    echo
    echo "First time only: open Dasher D200 from Finder. If macOS asks about"
    echo "the local network, click Allow. If it doesn't ask and local addresses"
    echo "still fail, go to System Settings > Privacy & Security > Local Network"
    echo "and switch Dasher D200 off and on again."
else
    echo "The app was signed but didn't verify - please report this."
fi
read -r -p "Press Return to close." _
