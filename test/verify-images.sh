#!/bin/bash
# test/verify-images.sh

echo "Checking image sizes..."
docker images | grep aibrix

echo "Verifying static binaries..."

# Improved verification function
verify_static() {
    local img=$1
    local bin=$2
    echo "Verifying $img..."
    
    # Extract binary from container for local verification
    id=$(docker create $img)
    if docker cp $id:$bin ./temp_bin 2>/dev/null; then
        docker rm -v $id >/dev/null
        
        if ldd ./temp_bin 2>&1 | grep -q "not a dynamic executable"; then
            echo "  ✓ $bin is statically linked."
        else
            echo "  ✗ $bin has dynamic dependencies:"
            ldd ./temp_bin
            rm ./temp_bin
            exit 1
        fi
        rm ./temp_bin
    else
        docker rm -v $id >/dev/null
        echo "  ✗ Failed to extract binary $bin from $img"
        exit 1
    fi
}

# Verify each component
verify_static "${CONTROLLER_IMG}" "/manager"
verify_static "${GATEWAY_IMG}" "/gateway-plugins"
verify_static "${METADATA_IMG}" "/metadata-service"