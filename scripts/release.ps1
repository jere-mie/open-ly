# Check if GitHub CLI is installed
if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
    Write-Host "GitHub CLI (gh) is not installed. Please install it first."
    exit 1
}

# Check if version.txt exists
if (-not (Test-Path "version.txt")) {
    Write-Host "version.txt not found. Please create it with the release version."
    exit 1
}

# Read the tag from version.txt
$tag = Get-Content "version.txt" | ForEach-Object { $_.Trim() }
$releaseName = "Release $tag"

Write-Host "Creating GitHub release for tag: $tag"

# Create the release
$releaseResult = gh release create "$tag" bin/* --title "$releaseName" --notes "Automated release for $tag"

if ($?) {
    Write-Host "GitHub release created successfully."
} else {
    Write-Host "Failed to create GitHub release."
    exit 1
}
