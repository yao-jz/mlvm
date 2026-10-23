//! A one-line probe: write a byte to an absolute address and see what the VM does with it.
//!
//! Useful for checking that a region of the 2^64 address space is mapped and writable before
//! relying on it. `0x3000…` is the float sub-game's I/O window — see
//! `rvgo/fast/floatsubgame.go`.

use std::ptr;

fn main() {
    // The address to poke. Any 64-bit address the guest is allowed to touch works here.
    let address: *mut u8 = 0x3000000000000000 as *mut u8;

    let data: u8 = 0xAB;

    unsafe {
        ptr::write_volatile(address, data);
    }

    println!("Wrote byte {} to address {:p}", data, address);
}
