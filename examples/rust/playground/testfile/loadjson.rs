// read a json file and format print it  --- passed
extern crate serde_json;
use std::fs::File;
use std::io::Read;

fn main() {
    let mut file = File::open("test.json").unwrap();
    let mut contents = String::new();
    file.read_to_string(&mut contents).unwrap();
    let json: serde_json::Value = serde_json::from_str(&contents).unwrap();
    println!("{}", serde_json::to_string_pretty(&json).unwrap());
}
