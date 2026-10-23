// loaf a file main.go into string and print it      --- passed
extern crate rand;
use std::fs::File;
use std::io::Read;

fn main() {
    let mut file = File::open("testfile").unwrap();
    let mut contents = String::new();
    file.read_to_string(&mut contents).unwrap();
    println!("The contents in the file is {}", contents);
    // print 10 random numbers
    for _ in 0..10 {
        println!("{}", rand::random::<u32>());
    }
}
