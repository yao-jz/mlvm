
// #![no_std]
// #![no_main]
// #![feature(restricted_std)]

// // use core::panic::PanicInfo;

// #[no_mangle]
pub extern "C" fn main() -> f32 {
    let _a: f32 = 1.2;
    let _b: f32 = _a * _a;
    return _b;
    let mut _vec = [0.; 4];
    _vec[0] = _a;
    for i in 1..3 {
        _vec[i] *= _vec[i-1];
    }
    println!("Hello, world!"); // because os = none is not supported, we can not use println! here even with std
    // write to file
    // let mut file = std::fs::File::create("foo.txt").unwrap();
    // file.write_all(b"Hello, world!").unwrap();
    loop {}
    // std::process::exit(0x12);
    // unsafe {
    //     asm!(
    //         "li a7, 93", // 60 is the syscall number for exit in x86_64
    //         "li a0, 1",  // 0 is the exit status
    //         "ecall"
    //     );
    //     loop {} // Prevent rust from generating invalid instructions after the syscall
    // }
}

// panic 
// #[panic_handler]
// fn panic(_info: &PanicInfo) -> ! {
//     loop {}
// }
