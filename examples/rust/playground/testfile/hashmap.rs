use std::collections::HashMap;

fn main() {
    let mut map = HashMap::new();

    map.insert("apple", 3);
    map.insert("banana", 2);
    map.insert("orange", 5);

    assert_eq!(map.get("apple"), Some(&3));
    assert_eq!(map.get("banana"), Some(&2));
    assert_eq!(map.get("orange"), Some(&5));

    println!("HashMap pass the test!");
}
